package domain

import "testing"

func TestYunxiaoExternalClosed(t *testing.T) {
	for _, state := range []string{"DONE", "closed", "已完成", "REJECTED", "已拒绝"} {
		issue := Issue{Source: "yunxiao", ExternalRef: &ExternalRef{ExternalState: state}}
		if !issue.ExternalClosed() {
			t.Fatalf("state %q should be closed", state)
		}
	}
	for _, state := range []string{"TODO", "IN_PROGRESS", "待处理"} {
		issue := Issue{Source: "yunxiao", ExternalRef: &ExternalRef{ExternalState: state}}
		if issue.ExternalClosed() {
			t.Fatalf("state %q must stay open", state)
		}
	}
}
