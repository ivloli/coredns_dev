package ecs_normalizer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/request"
	"github.com/dgraph-io/ristretto"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/miekg/dns"
)

// ECSNormalizer is a CoreDNS plugin that normalizes EDNS Client Subnet (ECS)
// options by mapping client IPs to fixed representative subnets per province+ISP.
//
// Flow per request:
//  1. Extract client IP from ECS option (or fallback to connection IP).
//  2. ip2region lookup → province + ISP.
//  3. Check Ristretto DNS response cache (province|isp|qname|qtype).
//  4. If miss: look up fixed subnet from Nacos-loaded sync.Map.
//  5. Inject/overwrite ECS option with the representative subnet.
//  6. Forward to next plugin (forward → PowerDNS).
//  7. Cache DNS response in Ristretto; return to client.
type ECSNormalizer struct {
	Next plugin.Handler
	cfg  *Config

	mu       sync.RWMutex
	searcher *xdb.Searcher // ip2region in-memory searcher (protected by mu)

	subnetMap sync.Map        // "province|isp" → subnet CIDR string
	dnsCache  *ristretto.Cache // DNS response cache
}

type cachedResponse struct {
	msg       *dns.Msg
	expiresAt time.Time
}

func (e *ECSNormalizer) Name() string { return "ecs_normalizer" }

func (e *ECSNormalizer) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	if len(r.Question) == 0 {
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}

	// 1. Extract client IP (prefer ECS option source address).
	state := request.Request{W: w, Req: r}
	clientIP := extractClientIP(state, r)

	// 2. ip2region lookup.
	e.mu.RLock()
	regionStr, err := e.searcher.SearchByStr(clientIP)
	e.mu.RUnlock()
	if err != nil || regionStr == "" {
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}

	province, isp := parseRegion(regionStr)
	if province == "" || isp == "" {
		// Overseas or unknown — passthrough without ECS injection.
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}

	// 3. Check DNS response cache.
	qname := r.Question[0].Name
	qtype := r.Question[0].Qtype
	cacheKey := fmt.Sprintf("%s|%s|%s|%d", province, isp, qname, qtype)

	if val, ok := e.dnsCache.Get(cacheKey); ok {
		if cr, ok := val.(*cachedResponse); ok && time.Now().Before(cr.expiresAt) {
			resp := cr.msg.Copy()
			resp.Id = r.Id
			w.WriteMsg(resp)
			return dns.RcodeSuccess, nil
		}
	}

	// 4. Look up fixed representative subnet.
	subnetVal, ok := e.subnetMap.Load(province + "|" + isp)
	if !ok {
		// No mapping in Nacos yet — passthrough.
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}
	subnet := subnetVal.(string)

	// 5. Clone request and inject ECS.
	rClone := r.Copy()
	if err := injectECS(rClone, subnet, e.cfg.PrefixLength); err != nil {
		log.Warningf("inject ECS failed (subnet=%s): %v", subnet, err)
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}

	// 6. Forward to next plugin.
	rec := dnstest.NewRecorder(w)
	rcode, err := plugin.NextOrFailure(e.Name(), e.Next, ctx, rec, rClone)
	if err != nil {
		return rcode, err
	}

	// 7. Cache response and write back to client.
	if rec.Msg != nil {
		if len(rec.Msg.Answer) > 0 {
			if ttl := getMinTTL(rec.Msg); ttl > 0 {
				cr := &cachedResponse{
					msg:       rec.Msg.Copy(),
					expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
				}
				packed, _ := rec.Msg.Pack()
				e.dnsCache.Set(cacheKey, cr, int64(len(packed)))
			}
		}
		w.WriteMsg(rec.Msg)
	}
	return rcode, nil
}
