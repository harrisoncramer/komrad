package komradlint

import (
	"komradlint/config"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("komradlint", func(settings any) (register.LinterPlugin, error) {
		decoded, err := register.DecodeSettings[config.Config](settings)
		if err != nil {
			return nil, err
		}

		return &linter{config: decoded}, nil
	})
}

// linter is the plugin golangci-lint holds while it runs.
type linter struct {
	config config.Config
}

// BuildAnalyzers returns every analyzer this plugin contributes.
func (l *linter) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	sentinels, err := newSentinelKind(l.config.SentinelKind)
	if err != nil {
		return nil, err
	}

	inline, err := newSentinelInline(l.config.SentinelInline)
	if err != nil {
		return nil, err
	}

	phrases, err := newWrapPhrase(l.config.WrapPhrase)
	if err != nil {
		return nil, err
	}

	handlers, err := newHandlerShape(l.config.HandlerShape)
	if err != nil {
		return nil, err
	}

	bare, err := newBareHTTP(l.config.BareHTTP)
	if err != nil {
		return nil, err
	}

	conversions, err := newConversionName(l.config.ConversionName)
	if err != nil {
		return nil, err
	}

	mappings, err := newSingleAppend(l.config.SingleAppend)
	if err != nil {
		return nil, err
	}

	params, err := newParamsShape(l.config.ParamsShape)
	if err != nil {
		return nil, err
	}

	boxes, err := newBlackBox(l.config.BlackBox)
	if err != nil {
		return nil, err
	}

	stores, err := newFakeStore(l.config.FakeStore)
	if err != nil {
		return nil, err
	}

	beside, err := newTestBeside(l.config.TestBeside)
	if err != nil {
		return nil, err
	}

	bound, err := newBoundKey(l.config.BoundKey)
	if err != nil {
		return nil, err
	}

	sleeps, err := newCtxSleep(l.config.CtxSleep)
	if err != nil {
		return nil, err
	}

	fatals, err := newTestFatalErr(l.config.TestFatalErr)
	if err != nil {
		return nil, err
	}

	texts, err := newErrorText(l.config.ErrorText)
	if err != nil {
		return nil, err
	}

	queries, err := newStoreQuery(l.config.StoreQuery)
	if err != nil {
		return nil, err
	}

	transactions, err := newTxStore(l.config.TxStore)
	if err != nil {
		return nil, err
	}

	services, err := newOneService(l.config.OneService)
	if err != nil {
		return nil, err
	}

	splits, err := newViewSplit(l.config.ViewSplit)
	if err != nil {
		return nil, err
	}

	return []*analysis.Analyzer{
		sentinels,
		inline,
		phrases,
		handlers,
		newSecretExpose(l.config.SecretExpose),
		bare,
		newOnelineFunc(l.config.OnelineFunc),
		conversions,
		mappings,
		params,
		boxes,
		stores,
		beside,
		bound,
		sleeps,
		fatals,
		texts,
		queries,
		transactions,
		services,
		splits,
	}, nil
}

// GetLoadMode tells golangci-lint the analyzers need types resolved.
func (l *linter) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
