package mcp

// 一些 JSON Schema 片段构造 helper，避免每个工具都写一坨 map。

func schemaObject(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	} else {
		s["required"] = []string{}
	}
	s["additionalProperties"] = false
	return s
}

func schemaString(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func schemaInt(desc string, min, max int) map[string]any {
	m := map[string]any{"type": "integer", "description": desc}
	if min != 0 || max != 0 {
		m["minimum"] = min
		m["maximum"] = max
	}
	return m
}

func schemaBool(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func schemaEnum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

func emptyObjectSchema() map[string]any {
	return schemaObject(map[string]any{})
}
