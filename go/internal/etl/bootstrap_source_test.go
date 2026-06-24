package etl

import "testing"

// Admin StartBootstrap диспетчеризует по source (premaster|glmf|contract).
func TestBootstrapRunnerFor(t *testing.T) {
	for _, s := range []string{"", "premaster", "glmf", "contract"} {
		if fn, ok := bootstrapRunnerFor(s); !ok || fn == nil {
			t.Errorf("bootstrapRunnerFor(%q) = (%v,%v), ожидался валидный runner", s, fn, ok)
		}
	}
	if fn, ok := bootstrapRunnerFor("nope"); ok || fn != nil {
		t.Errorf("bootstrapRunnerFor(unknown) должен вернуть (nil,false)")
	}
}
