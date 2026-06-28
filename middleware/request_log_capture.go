package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

const maxRequestLogResponseBodyBytes = 1024 * 1024

type requestLogResponseWriter struct {
	gin.ResponseWriter
	capture *common.RequestLogResponseCapture
}

func (w *requestLogResponseWriter) Write(body []byte) (int, error) {
	w.capture.AppendBody(body)
	return w.ResponseWriter.Write(body)
}

func (w *requestLogResponseWriter) WriteString(body string) (int, error) {
	return w.Write([]byte(body))
}

func RequestLogCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
		if ok && !settings.SaveRequestLog {
			c.Next()
			return
		}

		capture := common.NewRequestLogResponseCapture(c.Writer.Header(), maxRequestLogResponseBodyBytes)
		c.Set(common.KeyRequestLogResponseCapture, capture)
		c.Writer = &requestLogResponseWriter{
			ResponseWriter: c.Writer,
			capture:        capture,
		}
		c.Next()
	}
}
