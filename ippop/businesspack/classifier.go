package businesspack

import (
	"net"
	"sort"
	"strings"
)

const (
	PackGeneralWeb       = "general_web"
	PackYouTubeStreaming = "youtube_streaming"
	PackVideoStreaming   = "video_streaming_other"
	PackCodeDownload     = "code_download"
	PackImageCDN         = "image_cdn"
	PackCommerceWeb      = "commerce_web"
	PackSocialMedia      = "social_media"
	PackResearchAndNews  = "research_news"

	LabelAllow   = "allow"
	LabelGray    = "gray"
	LabelDeny    = "deny"
	LabelUnknown = "unknown"

	MatchTypeExact  = "exact"
	MatchTypeSuffix = "suffix"
)

type Rule struct {
	MatchType string
	Pattern   string
	Pack      string
}

type Classifier struct {
	rules       []Rule
	defaultPack string
}

func NewClassifier(customRules []Rule, defaultPack string) *Classifier {
	defaultPack = NormalizePack(defaultPack)
	if defaultPack == "" {
		defaultPack = PackGeneralWeb
	}

	rules := make([]Rule, 0, len(customRules)+len(defaultRules))
	for _, rule := range customRules {
		normalized := normalizeRule(rule)
		if normalized.Pattern == "" || normalized.Pack == "" {
			continue
		}
		rules = append(rules, normalized)
	}
	for _, rule := range defaultRules {
		rules = append(rules, normalizeRule(rule))
	}

	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].MatchType != rules[j].MatchType {
			return rules[i].MatchType == MatchTypeExact
		}
		return len(rules[i].Pattern) > len(rules[j].Pattern)
	})

	return &Classifier{rules: rules, defaultPack: defaultPack}
}

func (c *Classifier) Classify(host string, requestedPack string) string {
	if pack := NormalizePack(requestedPack); pack != "" {
		return pack
	}

	host = normalizeHost(host)
	if host == "" || net.ParseIP(host) != nil {
		return c.defaultPack
	}

	for _, rule := range c.rules {
		switch rule.MatchType {
		case MatchTypeExact:
			if host == rule.Pattern {
				return rule.Pack
			}
		default:
			if host == rule.Pattern || strings.HasSuffix(host, "."+rule.Pattern) {
				return rule.Pack
			}
		}
	}

	return c.defaultPack
}

func NormalizePack(pack string) string {
	pack = strings.ToLower(strings.TrimSpace(pack))
	if pack == "" {
		return ""
	}

	switch strings.ReplaceAll(pack, "-", "_") {
	case "youtube", "youtube_streaming":
		return PackYouTubeStreaming
	case "video", "video_streaming_other", "bilibili":
		return PackVideoStreaming
	case "code", "code_download", "github":
		return PackCodeDownload
	case "image", "cdn", "image_cdn":
		return PackImageCDN
	case "commerce", "commerce_web", "amazon":
		return PackCommerceWeb
	case "social", "social_media":
		return PackSocialMedia
	case "research", "news", "research_news":
		return PackResearchAndNews
	case "general", "general_web", "unknown":
		return PackGeneralWeb
	default:
		return strings.ReplaceAll(pack, "-", "_")
	}
}

func normalizeRule(rule Rule) Rule {
	matchType := strings.ToLower(strings.TrimSpace(rule.MatchType))
	if matchType != MatchTypeExact {
		matchType = MatchTypeSuffix
	}

	return Rule{
		MatchType: matchType,
		Pattern:   normalizeHost(rule.Pattern),
		Pack:      NormalizePack(rule.Pack),
	}
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return ""
	}

	if strings.HasPrefix(host, "[") && strings.Contains(host, "]") {
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = strings.Trim(parsedHost, "[]")
		}
	}
	if strings.Count(host, ":") == 1 {
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
	}
	return strings.TrimPrefix(host, ".")
}

var defaultRules = []Rule{
	{MatchType: MatchTypeSuffix, Pattern: "youtube.com", Pack: PackYouTubeStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "youtube-nocookie.com", Pack: PackYouTubeStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "googlevideo.com", Pack: PackYouTubeStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "ytimg.com", Pack: PackYouTubeStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "youtu.be", Pack: PackYouTubeStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "bilibili.com", Pack: PackVideoStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "bilivideo.com", Pack: PackVideoStreaming},
	{MatchType: MatchTypeSuffix, Pattern: "hdslb.com", Pack: PackVideoStreaming},
	{MatchType: MatchTypeExact, Pattern: "github.com", Pack: PackCodeDownload},
	{MatchType: MatchTypeSuffix, Pattern: "githubusercontent.com", Pack: PackCodeDownload},
	{MatchType: MatchTypeSuffix, Pattern: "githubassets.com", Pack: PackCodeDownload},
	{MatchType: MatchTypeSuffix, Pattern: "github.io", Pack: PackCodeDownload},
	{MatchType: MatchTypeSuffix, Pattern: "cloudfront.net", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "akamaihd.net", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "akamai.net", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "squarespace-cdn.com", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "dreamstime.com", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "alamy.com", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "walmartimages.com", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "ssl-images-amazon.com", Pack: PackImageCDN},
	{MatchType: MatchTypeSuffix, Pattern: "amazon.com", Pack: PackCommerceWeb},
	{MatchType: MatchTypeSuffix, Pattern: "ebayimg.com", Pack: PackCommerceWeb},
	{MatchType: MatchTypeSuffix, Pattern: "thecollective.in", Pack: PackCommerceWeb},
	{MatchType: MatchTypeSuffix, Pattern: "reddit.com", Pack: PackSocialMedia},
	{MatchType: MatchTypeSuffix, Pattern: "instagram.com", Pack: PackSocialMedia},
	{MatchType: MatchTypeSuffix, Pattern: "tiktok.com", Pack: PackSocialMedia},
	{MatchType: MatchTypeSuffix, Pattern: "blogger.googleusercontent.com", Pack: PackSocialMedia},
	{MatchType: MatchTypeSuffix, Pattern: "researchgate.net", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "pnas.org", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "st-andrews.ac.uk", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "acs.org", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "ctvnews.ca", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "nytimes.com", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "glamour.com", Pack: PackResearchAndNews},
	{MatchType: MatchTypeSuffix, Pattern: "chasingcars.com.au", Pack: PackResearchAndNews},
}
