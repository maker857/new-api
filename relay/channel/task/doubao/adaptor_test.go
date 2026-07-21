package doubao

import "testing"

func TestBuildTaskURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "root endpoint",
			baseURL: "https://ark.cn-beijing.volces.com",
			want:    "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
		},
		{
			name:    "plan endpoint",
			baseURL: "https://ark.cn-beijing.volces.com/api/plan",
			want:    "https://ark.cn-beijing.volces.com/api/plan/v3/contents/generations/tasks",
		},
		{
			name:    "plan endpoint with trailing slash",
			baseURL: "https://ark.cn-beijing.volces.com/api/plan/",
			want:    "https://ark.cn-beijing.volces.com/api/plan/v3/contents/generations/tasks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTaskURL(tt.baseURL); got != tt.want {
				t.Errorf("buildTaskURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
