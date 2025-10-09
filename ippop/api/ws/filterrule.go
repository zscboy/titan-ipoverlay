package ws

import (
	"fmt"
	"titan-ipoverlay/ippop/api/internal/config"
)

const (
	RuleActionAllow = "allow"
	RuleActionDeny  = "deny"

	RuleTypeDomain = "domain"
	RuleTypeIP     = "ip"
	RuleTypePort   = "port"
)

func RulesToMap(rules []config.FilterRule) map[string]*config.FilterRule {
	ruleMap := make(map[string]*config.FilterRule)
	for _, rule := range rules {
		ruleMap[rule.Value] = &rule
	}
	return ruleMap
}

type Rules struct {
	rules         map[string]*config.FilterRule
	defaultAction string
}

func (rules *Rules) isDeny(domain, port string) bool {
	rule := rules.rules[domain]
	if rule == nil {
		rule = rules.rules[port]
	}

	if rule == nil {
		return rules.defaultAction == RuleActionDeny
	}

	if rule.Type != RuleTypeDomain && rule.Type != RuleTypeIP && rule.Type != RuleTypePort {
		panic(fmt.Sprintf("unsupport rule type %s", rule.Type))
	}

	if rule.Action != RuleActionAllow && rule.Action != RuleActionDeny {
		panic(fmt.Sprintf("unsupport rule action %s", rule.Action))
	}

	return rule.Action == RuleActionDeny
}
