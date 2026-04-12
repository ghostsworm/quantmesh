package llm

import "testing"

func TestStreamChunkUsage_Root(t *testing.T) {
	chunk := map[string]interface{}{
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(42),
			"candidatesTokenCount": float64(7),
		},
	}
	in, out, ok := streamChunkUsage(chunk)
	if !ok || in != 42 || out != 7 {
		t.Fatalf("got in=%d out=%d ok=%v", in, out, ok)
	}
}

func TestStreamChunkUsage_CandidateFallback(t *testing.T) {
	chunk := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"usageMetadata": map[string]interface{}{
					"promptTokenCount":     float64(10),
					"candidatesTokenCount": float64(3),
				},
			},
		},
	}
	in, out, ok := streamChunkUsage(chunk)
	if !ok || in != 10 || out != 3 {
		t.Fatalf("got in=%d out=%d ok=%v", in, out, ok)
	}
}
