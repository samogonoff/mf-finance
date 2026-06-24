package etl

import "context"

// bootstrapRunner — сигнатура заливки одного ЮЛ.
type bootstrapRunner func(context.Context, Deps, BootstrapOpts) (int64, error)

// bootstrapRunnerFor выбирает заливку по источнику (admin StartBootstrap / CLI):
//   premaster (default) → fact_premaster; glmf → fact_glmf; contract → dim_contract.
func bootstrapRunnerFor(source string) (bootstrapRunner, bool) {
	switch source {
	case "", "premaster":
		return RunBootstrap, true
	case "glmf":
		return RunBootstrapGLMF, true
	case "contract":
		return RunBootstrapContract, true
	default:
		return nil, false
	}
}
