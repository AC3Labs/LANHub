package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"
	"unicode"
)

var templateFuncs = template.FuncMap{
	"humanSize":  humanSize,
	"humanSpeed": func(b float64) string { return humanSize(int64(math.Round(b))) + "/s" },
	"ago":        ago,
	"osName":     osName,
	"ucfirst":    ucfirst,
	"json": func(v any) template.JS {
		b, _ := json.Marshal(v)
		return template.JS(b)
	},
	"safeCSS": func(s string) template.CSS { return template.CSS(s) },
	"navItem": func(href, label string, active bool) map[string]any {
		return map[string]any{"Href": href, "Label": label, "Active": active}
	},
	// dict builds a map for passing several values to a sub-template.
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[fmt.Sprint(kv[i])] = kv[i+1]
		}
		return m
	},
	"color":   safeColor,
	"eq2":     func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
	"basename": func(p string) string {
		p = strings.ReplaceAll(p, `\`, "/")
		if i := strings.LastIndex(strings.TrimRight(p, "/"), "/"); i >= 0 {
			return strings.TrimRight(p, "/")[i+1:]
		}
		return p
	},
}

// humanSize mirrors the PHP helper human_filesize(): 1024 steps, one
// decimal place above bytes.
func humanSize(b int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	if b < 0 {
		b = 0
	}
	power := 0
	if b > 0 {
		power = int(math.Floor(math.Log(float64(b)) / math.Log(1024)))
	}
	if power > len(units)-1 {
		power = len(units) - 1
	}
	v := float64(b) / math.Pow(1024, float64(power))
	if power == 0 {
		return fmt.Sprintf("%d %s", b, units[0])
	}
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0") // PHP round() drops a trailing .0
	return s + " " + units[power]
}

// ago approximates Carbon's diffForHumans() ("3 minutes ago").
func ago(t any) string {
	var tt time.Time
	switch v := t.(type) {
	case time.Time:
		tt = v
	case *time.Time:
		if v == nil {
			return ""
		}
		tt = *v
	case string:
		p, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return v
		}
		tt = p
	default:
		return ""
	}
	d := time.Since(tt)
	suffix := "ago"
	if d < 0 {
		d, suffix = -d, "from now"
	}
	unit := func(n int, name string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s %s", name, suffix)
		}
		return fmt.Sprintf("%d %ss %s", n, name, suffix)
	}
	switch {
	case d < time.Second:
		return unit(1, "second")
	case d < time.Minute:
		return unit(int(d.Seconds()), "second")
	case d < time.Hour:
		return unit(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return unit(int(d.Hours()), "hour")
	case d < 7*24*time.Hour:
		return unit(int(d.Hours()/24), "day")
	case d < 30*24*time.Hour:
		return unit(int(d.Hours()/24/7), "week")
	case d < 365*24*time.Hour:
		return unit(int(d.Hours()/24/30), "month")
	default:
		return unit(int(d.Hours()/24/365), "year")
	}
}

func osName(os string) string {
	switch os {
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "macos":
		return "macOS"
	case "other":
		return "Other"
	}
	return ucfirst(os)
}

func ucfirst(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

// safeColor only lets a #rrggbb colour into inline styles.
func safeColor(c string) template.CSS {
	if len(c) == 7 && c[0] == '#' {
		ok := true
		for _, r := range c[1:] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				ok = false
			}
		}
		if ok {
			return template.CSS(c)
		}
	}
	return template.CSS("#8c7a63")
}
