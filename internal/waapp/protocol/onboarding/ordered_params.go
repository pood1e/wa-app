package onboarding

import (
	"strings"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

type OrderedParam struct {
	Key string
	Val string
	Raw bool
}

type OrderedParams []OrderedParam

func (p *OrderedParams) Set(key string, value string, raw bool) {
	if strings.TrimSpace(key) == "" {
		return
	}
	for i := range *p {
		if (*p)[i].Key == key {
			(*p)[i].Val = value
			(*p)[i].Raw = (*p)[i].Raw || raw
			return
		}
	}
	*p = append(*p, OrderedParam{Key: key, Val: value, Raw: raw})
}

func (p *OrderedParams) Remove(key string) {
	for i := range *p {
		if (*p)[i].Key == key {
			*p = append((*p)[:i], (*p)[i+1:]...)
			return
		}
	}
}

func (p OrderedParams) Get(key string) string {
	for _, item := range p {
		if item.Key == key {
			return item.Val
		}
	}
	return ""
}

func (p OrderedParams) RawKeys() []string {
	out := []string{}
	for _, item := range p {
		if item.Raw {
			out = append(out, item.Key)
		}
	}
	return out
}

func (p OrderedParams) ToMap() map[string]string {
	out := make(map[string]string, len(p))
	for _, item := range p {
		out[item.Key] = item.Val
	}
	return out
}

func (p OrderedParams) Render() string {
	parts := make([]string, 0, len(p))
	for _, item := range p {
		value := pstate.QuoteForm(item.Val)
		if item.Raw {
			value = item.Val
		}
		parts = append(parts, pstate.QuoteForm(item.Key)+"="+value)
	}
	return strings.Join(parts, "&")
}
