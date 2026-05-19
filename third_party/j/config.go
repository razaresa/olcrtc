package j

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const jitsiConfigFetchTimeout = 5 * time.Second

type jitsiDomains struct {
	MUCDomain   string
	FocusDomain string
}

func defaultJitsiDomains(host string) jitsiDomains {
	return jitsiDomains{
		MUCDomain:   "conference." + host,
		FocusDomain: "focus." + host,
	}
}

func resolveJitsiDomains(ctx context.Context, cfg Config) jitsiDomains {
	domains := defaultJitsiDomains(cfg.Host)
	if cfg.MUCDomain == "" || cfg.FocusDomain == "" {
		fetched := fetchJitsiConfigDomains(ctx, cfg.Host)
		if cfg.MUCDomain == "" && fetched.MUCDomain != "" {
			domains.MUCDomain = fetched.MUCDomain
		}
		if cfg.FocusDomain == "" && fetched.FocusDomain != "" {
			domains.FocusDomain = fetched.FocusDomain
		}
	}
	if cfg.MUCDomain != "" {
		domains.MUCDomain = cfg.MUCDomain
	}
	if cfg.FocusDomain != "" {
		domains.FocusDomain = cfg.FocusDomain
	}
	return domains
}

func fetchJitsiConfigDomains(ctx context.Context, host string) jitsiDomains {
	reqCtx, cancel := context.WithTimeout(ctx, jitsiConfigFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://"+host+"/config.js", nil)
	if err != nil {
		return jitsiDomains{}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return jitsiDomains{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jitsiDomains{}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return jitsiDomains{}
	}
	return parseJitsiConfigDomains(host, body)
}

func parseJitsiConfigDomains(host string, body []byte) jitsiDomains {
	domains := defaultJitsiDomains(host)
	js := stripLineComments(body)
	vars := jsStringVars(js)
	if value := firstJSStringExpressionProperty(js, "muc", vars); value != "" {
		domains.MUCDomain = value
	}
	if value := firstJSStringExpressionProperty(js, "focus", vars); value != "" {
		domains.FocusDomain = value
	}
	return domains
}

func stripLineComments(body []byte) []byte {
	lines := bytes.Split(body, []byte{'\n'})
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("//")) {
			continue
		}
		out = append(out, line)
	}
	return bytes.Join(out, []byte{'\n'})
}

func jsStringVars(body []byte) map[string]string {
	re := regexp.MustCompile(`(?m)\bvar\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*['"]([^'"]*)['"]\s*;`)
	vars := make(map[string]string)
	for _, match := range re.FindAllSubmatch(body, -1) {
		if len(match) == 3 {
			vars[string(match[1])] = string(match[2])
		}
	}
	return vars
}

func firstJSStringExpressionProperty(body []byte, key string, vars map[string]string) string {
	re := regexp.MustCompile(`(?m)(^|[\s,{])` + regexp.QuoteMeta(key) + `\s*:\s*([^,\n}]+)`)
	match := re.FindSubmatch(body)
	if len(match) < 3 {
		return ""
	}
	return evalJSStringExpression(strings.TrimSpace(string(match[2])), vars)
}

func evalJSStringExpression(expr string, vars map[string]string) string {
	var out strings.Builder
	for _, part := range strings.Split(expr, "+") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(part) >= 2 {
			quote := part[0]
			if (quote == '\'' || quote == '"') && part[len(part)-1] == quote {
				out.WriteString(part[1 : len(part)-1])
				continue
			}
		}
		value, ok := vars[part]
		if !ok {
			return ""
		}
		out.WriteString(value)
	}
	return strings.TrimSpace(out.String())
}
