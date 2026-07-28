package doubao

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type,omitempty"`
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           string         `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           string         `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	SafetyIdentifier string         `json:"safety_identifier,omitempty"`
	Priority         *dto.IntValue  `json:"priority,omitempty"`
	Resolution       string         `json:"resolution,omitempty"`
	Ratio            string         `json:"ratio,omitempty"`
	Duration         *dto.IntValue  `json:"duration,omitempty"`
	Frames           *dto.IntValue  `json:"frames,omitempty"`
	Seed             *dto.IntValue  `json:"seed,omitempty"`
	CameraFixed      *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark        *dto.BoolValue `json:"watermark,omitempty"`
	Extra            map[string]any `json:"-"`
}

func (r requestPayload) MarshalJSON() ([]byte, error) {
	type requestPayloadAlias requestPayload

	payloadJSON, err := common.Marshal(requestPayloadAlias(r))
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := common.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, err
	}

	for key, value := range r.Extra {
		switch key {
		case "model", "content", "duration":
			continue
		}
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}

	return common.Marshal(payload)
}

type responsePayload struct {
	ID string `json:"id"` // task_id
}

type responseTask struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	Tools           []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		ToolUsage        struct {
			WebSearch int `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if _, _, err := resolveSeedanceDimensions(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return buildTaskURL(a.baseURL), nil
}

func buildTaskURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api/plan") {
		return fmt.Sprintf("%s/v3/contents/generations/tasks", baseURL)
	}
	return fmt.Sprintf("%s/api/v3/contents/generations/tasks", baseURL)
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 根据请求 metadata 中的输出分辨率与是否包含视频输入，返回相对基准价的计费 OtherRatio。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolution, _, err := resolveSeedanceDimensions(&req)
	if err != nil {
		return nil
	}
	hasVideo := hasVideoInMetadata(req.Metadata)
	ratio, ok := GetVideoInputRatio(info.OriginModelName, resolution, hasVideo)
	if !ok || ratio == 1.0 {
		return nil
	}
	return map[string]float64{"video_input": ratio}
}

// hasVideoInMetadata 直接检查 metadata 的 content 数组是否包含 video_url 条目，
// 避免构建完整的上游 requestPayload。
func hasVideoInMetadata(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["type"] == "video_url" {
			return true
		}
		if _, has := itemMap["video_url"]; has {
			return true
		}
	}
	return false
}

func resolveSeedanceDimensions(req *relaycommon.TaskSubmitReq) (string, string, error) {
	resolution, _ := req.Metadata["resolution"].(string)
	ratio, _ := req.Metadata["ratio"].(string)
	if explicitResolution, ok := req.Extra["resolution"].(string); ok && explicitResolution != "" {
		resolution = explicitResolution
	}
	if explicitRatio, ok := req.Extra["ratio"].(string); ok && explicitRatio != "" {
		ratio = explicitRatio
	}

	widthValue, hasWidth := req.Extra["width"]
	heightValue, hasHeight := req.Extra["height"]
	if (!hasWidth && !hasHeight) || (resolution != "" && ratio != "") {
		return resolution, ratio, nil
	}
	if !hasWidth || !hasHeight {
		return "", "", fmt.Errorf("Seedance width and height must be specified together")
	}

	width, widthErr := strconv.Atoi(common.Interface2String(widthValue))
	height, heightErr := strconv.Atoi(common.Interface2String(heightValue))
	if widthErr != nil || heightErr != nil {
		return "", "", fmt.Errorf("invalid Seedance dimensions %v x %v", widthValue, heightValue)
	}

	switch {
	case width == 1920 && height == 1080:
		if resolution == "" {
			resolution = "1080p"
		}
		if ratio == "" {
			ratio = "16:9"
		}
	case width == 1080 && height == 1920:
		if resolution == "" {
			resolution = "1080p"
		}
		if ratio == "" {
			ratio = "9:16"
		}
	case width == 1280 && height == 720:
		if resolution == "" {
			resolution = "720p"
		}
		if ratio == "" {
			ratio = "16:9"
		}
	case width == 720 && height == 1280:
		if resolution == "" {
			resolution = "720p"
		}
		if ratio == "" {
			ratio = "9:16"
		}
	default:
		return "", "", fmt.Errorf("unsupported Seedance dimensions %dx%d", width, height)
	}

	return resolution, ratio, nil
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}
	if common.GetContextKeyBool(c, constant.ContextKeyNativeSeedanceResponse) {
		c.Data(http.StatusOK, "application/json", responseBody)
		return dResp.ID, responseBody, nil
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/%s", buildTaskURL(baseUrl), taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// Add images if present
	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
			})
		}
	}

	metadata := make(map[string]any, len(req.Metadata))
	for key, value := range req.Metadata {
		switch key {
		case "model", "duration", "content":
			continue
		}
		metadata[key] = value
	}
	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}
	if contentRaw, ok := req.Metadata["content"]; ok {
		contentJSON, err := common.Marshal(contentRaw)
		if err != nil {
			return nil, errors.Wrap(err, "marshal metadata content failed")
		}
		var metadataContent []ContentItem
		if err := common.Unmarshal(contentJSON, &metadataContent); err != nil {
			return nil, errors.Wrap(err, "unmarshal metadata content failed")
		}
		for _, content := range metadataContent {
			switch content.Type {
			case "image_url", "video_url", "audio_url":
				r.Content = append(r.Content, content)
			}
		}
	}

	resolution, ratio, err := resolveSeedanceDimensions(req)
	if err != nil {
		return nil, err
	}
	r.Resolution = resolution
	r.Ratio = ratio
	if len(req.Extra) > 0 {
		r.Extra = make(map[string]any, len(req.Extra))
		for key, value := range req.Extra {
			if key == "width" || key == "height" {
				continue
			}
			r.Extra[key] = value
		}
	}

	duration := req.Duration
	if duration == 0 {
		duration, _ = strconv.Atoi(req.Seconds)
	}
	if duration != 0 {
		r.Duration = lo.ToPtr(dto.IntValue(duration))
	}

	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	r.Content = append(r.Content, ContentItem{
		Type: "text",
		Text: req.Prompt,
	})

	return &r, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.SetMetadata("url", dResp.Content.VideoURL)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: dResp.Error.Message,
			Code:    dResp.Error.Code,
		}
	}

	return common.Marshal(openAIVideo)
}
