package domain

import "testing"

func TestRole_Valid(t *testing.T) {
	cases := []struct {
		role Role
		want bool
	}{
		{RoleAdmin, true},
		{RoleEditor, true},
		{RoleViewer, true},
		{Role(""), false},
		{Role("super"), false}, // is_super é flag, não role
		{Role("guest"), false},
	}
	for _, c := range cases {
		if got := c.role.Valid(); got != c.want {
			t.Errorf("role=%q: esperado %v, veio %v", c.role, c.want, got)
		}
	}
}

func TestRole_AtLeastEditor(t *testing.T) {
	// admin e editor têm privilégio de escrita colaborativa; viewer não.
	if !RoleAdmin.AtLeastEditor() {
		t.Error("admin deve satisfazer AtLeastEditor")
	}
	if !RoleEditor.AtLeastEditor() {
		t.Error("editor deve satisfazer AtLeastEditor")
	}
	if RoleViewer.AtLeastEditor() {
		t.Error("viewer NÃO deve satisfazer AtLeastEditor")
	}
	if Role("").AtLeastEditor() {
		t.Error("role vazia NÃO deve satisfazer AtLeastEditor")
	}
}

func TestFeedbackType_Valid(t *testing.T) {
	for _, tp := range []FeedbackType{FeedbackBug, FeedbackSuggestion, FeedbackPraise, FeedbackOther} {
		if !tp.Valid() {
			t.Errorf("tipo %q deveria ser válido", tp)
		}
	}
	for _, tp := range []FeedbackType{"", "spam", "complaint"} {
		if tp.Valid() {
			t.Errorf("tipo %q não deveria ser válido", tp)
		}
	}
}

func TestFeedbackStatus_Valid(t *testing.T) {
	for _, s := range []FeedbackStatus{FeedbackNew, FeedbackTriaging, FeedbackResolved, FeedbackDismissed} {
		if !s.Valid() {
			t.Errorf("status %q deveria ser válido", s)
		}
	}
	for _, s := range []FeedbackStatus{"", "pending", "closed"} {
		if s.Valid() {
			t.Errorf("status %q não deveria ser válido", s)
		}
	}
}

func TestAnomalyMetric_Valid(t *testing.T) {
	for _, m := range []AnomalyMetric{AnomalyErrorCount, AnomalyEventCount, AnomalyErrorRate} {
		if !m.Valid() {
			t.Errorf("metric %q deveria ser válido", m)
		}
	}
	for _, m := range []AnomalyMetric{"", "latency_p95", "throughput"} {
		if m.Valid() {
			t.Errorf("metric %q não deveria ser válido", m)
		}
	}
}

func TestAnomalyDirection_Valid(t *testing.T) {
	for _, d := range []AnomalyDirection{AnomalyAbove, AnomalyBelow, AnomalyBoth} {
		if !d.Valid() {
			t.Errorf("direction %q deveria ser válido", d)
		}
	}
	for _, d := range []AnomalyDirection{"", "left", "right"} {
		if d.Valid() {
			t.Errorf("direction %q não deveria ser válido", d)
		}
	}
}

func TestAlertChannel_Valid(t *testing.T) {
	if !AlertChannelSlack.Valid() {
		t.Error("slack deveria ser válido")
	}
	if !AlertChannelWebhook.Valid() {
		t.Error("webhook deveria ser válido")
	}
	for _, c := range []AlertChannel{"", "email", "sms", "teams"} {
		if c.Valid() {
			t.Errorf("channel %q não deveria ser válido", c)
		}
	}
}
