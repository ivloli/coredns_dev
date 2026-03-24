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

	qname := r.Question[0].Name

	// 1. Extract client IP (prefer ECS option source address).
	state := request.Request{W: w, Req: r}
	clientIP := extractClientIP(state, r)
	log.Debugf("[%s] client_ip=%s", qname, clientIP)

	// 2. ip2region lookup.
	e.mu.RLock()
	regionStr, err := e.searcher.SearchByStr(clientIP)
	e.mu.RUnlock()
	if err != nil || regionStr == "" {
		log.Debugf("[%s] ip2region miss for %s, passthrough", qname, clientIP)
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}

	province, isp := parseRegion(regionStr)
	log.Debugf("[%s] ip2region result: %s → province=%s isp=%s", qname, clientIP, province, isp)
	if province == "" || isp == "" {
		// Overseas or unknown — passthrough without ECS injection.
		log.Debugf("[%s] unknown region, passthrough", qname)
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}

	// 3. Check DNS response cache.
	qtype := r.Question[0].Qtype
	cacheKey := fmt.Sprintf("%s|%s|%s|%d", province, isp, qname, qtype)

	if val, ok := e.dnsCache.Get(cacheKey); ok {
		if cr, ok := val.(*cachedResponse); ok && time.Now().Before(cr.expiresAt) {
			log.Debugf("[%s] cache hit (province=%s isp=%s)", qname, province, isp)
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
		log.Warningf("[%s] no subnet mapping for province=%s isp=%s, passthrough", qname, province, isp)
		return plugin.NextOrFailure(e.Name(), e.Next, ctx, w, r)
	}
	subnet := subnetVal.(string)
	log.Debugf("[%s] injecting ECS subnet=%s (province=%s isp=%s)", qname, subnet, province, isp)

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
