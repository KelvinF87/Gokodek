package agent

import "testing"

func TestPlanToolAllowed(t *testing.T) {
	for _, name := range []string{"project_info", "diagnose_project", "list_dir", "read_file", "rag_search", "check_web", "browser_screenshot", "start_server", "plan_file"} {
		if !planToolAllowed(name) {
			t.Fatalf("expected %s allowed in plan mode", name)
		}
	}
	for _, name := range []string{"write_file", "run_cmd", "git", "tasks", "build", "run_test", "fetch_url"} {
		if planToolAllowed(name) {
			t.Fatalf("expected %s blocked in plan mode", name)
		}
	}
}
