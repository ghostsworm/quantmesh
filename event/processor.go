package event

// EventProcessor 事件处理器接口（避免循环依赖）
type EventProcessor interface {
	ProcessEvent(event *Event)
}

