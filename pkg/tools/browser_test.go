package tools

import "testing"

func TestDesktopControlsRequireExplicitPermission(t *testing.T) {
	if _, err := NewMouseClickTool(false).Execute(`{"x":10,"y":10}`); err == nil {
		t.Fatal("expected mouse control to require explicit permission")
	}
	if _, err := NewKeyboardTypeTool(false).Execute(`{"text":"hello"}`); err == nil {
		t.Fatal("expected keyboard control to require explicit permission")
	}
	if _, err := NewCaptureScreenTool(t.TempDir(), false).Execute(`{}`); err == nil {
		t.Fatal("expected desktop capture to require explicit permission")
	}
}
