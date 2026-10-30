package notify

import "quantmesh/event"

// eventBody 取出直发通知预先写好的正文。没有时返回空串，调用方继续按字段展开。
func eventBody(evt *event.Event) string {
	if evt == nil || evt.Data == nil {
		return ""
	}
	body, _ := evt.Data["message"].(string)
	return body
}
