package csrf

import (
	envoy_config_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_config_route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoycsrf "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/csrf/v3"
	envoy_type_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	envoytype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/rotisserie/eris"
	v3 "github.com/solo-io/gloo/projects/gloo/pkg/api/external/envoy/config/core/v3"
	csrf "github.com/solo-io/gloo/projects/gloo/pkg/api/external/envoy/extensions/filters/http/csrf/v3"
	gloo_type_matcher "github.com/solo-io/gloo/projects/gloo/pkg/api/external/envoy/type/matcher/v3"
	v1 "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins"
	"github.com/solo-io/gloo/projects/gloo/pkg/plugins/pluginutils"
)

// filter should be called after routing decision has been made
var pluginStage = plugins.DuringStage(plugins.RouteStage)

const FilterName = "envoy.filters.http.csrf"

func NewPlugin() *plugin {
	return &plugin{}
}

var _ plugins.Plugin = new(plugin)
var _ plugins.HttpFilterPlugin = new(plugin)
var _ plugins.WeightedDestinationPlugin = new(plugin)
var _ plugins.VirtualHostPlugin = new(plugin)
var _ plugins.RoutePlugin = new(plugin)

type plugin struct{}

func (p *plugin) Init(params plugins.InitParams) error {
	return nil
}

func (p *plugin) HttpFilters(_ plugins.Params, listener *v1.HttpListener) ([]plugins.StagedHttpFilter, error) {
	glooCsrfConfig := listener.GetOptions().GetCsrf()
	if glooCsrfConfig == nil {
		return nil, nil
	}

	envoyCsrfConfig, err := translateCsrfConfig(glooCsrfConfig)
	if err != nil {
		return nil, err
	}

	csrfFilter, err := plugins.NewStagedFilterWithConfig(FilterName, envoyCsrfConfig, pluginStage)
	if err != nil {
		return nil, eris.Wrap(err, "generating filter config")
	}

	return []plugins.StagedHttpFilter{csrfFilter}, nil
}

func (p *plugin) ProcessRoute(params plugins.RouteParams, in *v1.Route, out *envoy_config_route.Route) error {
	csrfPolicy := in.GetOptions().GetCsrf()
	if csrfPolicy == nil {
		return nil
	}

	envoyCsrfConfig, err := translateCsrfConfig(csrfPolicy)
	if err != nil {
		return err
	}

	return pluginutils.SetRoutePerFilterConfig(out, FilterName, envoyCsrfConfig)
}

func (p *plugin) ProcessVirtualHost(
	params plugins.VirtualHostParams,
	in *v1.VirtualHost,
	out *envoy_config_route.VirtualHost,
) error {
	csrfPolicy := in.GetOptions().GetCsrf()
	if csrfPolicy == nil {
		return nil
	}

	envoyCsrfConfig, err := translateCsrfConfig(csrfPolicy)
	if err != nil {
		return err
	}

	return pluginutils.SetVhostPerFilterConfig(out, FilterName, envoyCsrfConfig)
}

func (p *plugin) ProcessWeightedDestination(
	params plugins.RouteParams,
	in *v1.WeightedDestination,
	out *envoy_config_route.WeightedCluster_ClusterWeight,
) error {
	csrfPolicy := in.GetOptions().GetCsrf()
	if csrfPolicy == nil {
		return nil
	}

	envoyCsrfConfig, err := translateCsrfConfig(csrfPolicy)
	if err != nil {
		return err
	}

	return pluginutils.SetWeightedClusterPerFilterConfig(out, FilterName, envoyCsrfConfig)
}

func translateCsrfConfig(csrf *csrf.CsrfPolicy) (*envoycsrf.CsrfPolicy, error) {
	csrfPolicy := &envoycsrf.CsrfPolicy{
		FilterEnabled:     translateRuntimeFractionalPercent(csrf.GetFilterEnabled()),
		ShadowEnabled:     translateRuntimeFractionalPercent(csrf.GetShadowEnabled()),
		AdditionalOrigins: translateAdditionalOrigins(csrf.GetAdditionalOrigins()),
	}

	return csrfPolicy, csrfPolicy.Validate()
}

func translateRuntimeFractionalPercent(rfp *v3.RuntimeFractionalPercent) *envoy_config_core.RuntimeFractionalPercent {
	if rfp == nil {
		return &envoy_config_core.RuntimeFractionalPercent{
			DefaultValue: &envoytype.FractionalPercent{
				Numerator:   1,
				Denominator: envoytype.FractionalPercent_HUNDRED,
			},
		}
	}

	return &envoy_config_core.RuntimeFractionalPercent{
		DefaultValue: &envoytype.FractionalPercent{
			Numerator:   rfp.GetDefaultValue().GetNumerator(),
			Denominator: envoytype.FractionalPercent_DenominatorType(
				envoytype.FractionalPercent_DenominatorType_value[rfp.GetDefaultValue().GetDenominator().String()],
			),
		},
		RuntimeKey: rfp.GetRuntimeKey(),
	}
}

func translateAdditionalOrigins(glooAdditionalOrigins []*gloo_type_matcher.StringMatcher) []*envoy_type_matcher.StringMatcher {
	var envoyAdditionalOrigins []*envoy_type_matcher.StringMatcher

	for _, ao := range glooAdditionalOrigins {
		switch typed := ao.GetMatchPattern().(type) {
		case *gloo_type_matcher.StringMatcher_Exact:
			envoyAdditionalOrigins = append(envoyAdditionalOrigins, &envoy_type_matcher.StringMatcher{
				MatchPattern: &envoy_type_matcher.StringMatcher_Exact{
					Exact: typed.Exact,
				},
				IgnoreCase: ao.GetIgnoreCase(),
			})
		case *gloo_type_matcher.StringMatcher_Prefix:
			envoyAdditionalOrigins = append(envoyAdditionalOrigins, &envoy_type_matcher.StringMatcher{
				MatchPattern: &envoy_type_matcher.StringMatcher_Prefix{
					Prefix: typed.Prefix,
				},
				IgnoreCase: ao.GetIgnoreCase(),
			})
		case *gloo_type_matcher.StringMatcher_SafeRegex:
			envoyAdditionalOrigins = append(envoyAdditionalOrigins, &envoy_type_matcher.StringMatcher{
				MatchPattern: &envoy_type_matcher.StringMatcher_SafeRegex{
					SafeRegex: &envoy_type_matcher.RegexMatcher{
						EngineType: &envoy_type_matcher.RegexMatcher_GoogleRe2{
							GoogleRe2: &envoy_type_matcher.RegexMatcher_GoogleRE2{},
						},
						Regex: typed.SafeRegex.GetRegex(),
					},
				},
			})
		case *gloo_type_matcher.StringMatcher_Suffix:
			envoyAdditionalOrigins = append(envoyAdditionalOrigins, &envoy_type_matcher.StringMatcher{
				MatchPattern: &envoy_type_matcher.StringMatcher_Suffix{
					Suffix: typed.Suffix,
				},
				IgnoreCase: ao.GetIgnoreCase(),
			})
		}
	}

	return envoyAdditionalOrigins
}
