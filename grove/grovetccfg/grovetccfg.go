package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/apache/incubator-trafficcontrol/lib/go-tc"
	to "github.com/apache/incubator-trafficcontrol/traffic_ops/client"

	grove "github.com/apache/incubator-trafficcontrol/grove/cache"
)

const Version = "0.1"
const UserAgent = "grove-tc-cfg/" + Version
const TrafficOpsTimeout = time.Second * 90
const DefaultCertificateDir = "/etc/grove/ssl"

func AvailableStatuses() map[string]struct{} {
	return map[string]struct{}{
		"reported": struct{}{},
		"online":   struct{}{},
	}
}

func main() {
	toURL := flag.String("tourl", "", "The Traffic Ops URL")
	toUser := flag.String("touser", "", "The Traffic Ops username")
	toPass := flag.String("topass", "", "The Traffic Ops password")
	pretty := flag.Bool("pretty", false, "Whether to pretty-print output")
	host := flag.String("host", "", "The hostname of the server whose config to generate")
	// api := flag.String("api", "1.2", "API version. Determines whether to use /api/1.3/configs/ or older, less efficient 1.2 APIs")
	toInsecure := flag.Bool("insecure", false, "Whether to allow invalid certificates with Traffic Ops")
	certDir := flag.String("certdir", DefaultCertificateDir, "Directory to save certificates to")
	flag.Parse()

	useCache := false
	toc, _, err := to.LoginWithAgent(*toURL, *toUser, *toPass, *toInsecure, UserAgent, useCache, TrafficOpsTimeout)
	if err != nil {
		fmt.Printf("Error connecting to Traffic Ops: %v\n", err)
		os.Exit(1)
	}

	rules := grove.RemapRules{}
	// if *api == "1.3" {
	// 	rules, err = createRulesNewAPI(toc, *host, *certDir)
	// } else {
	rules, err = createRulesOldAPI(toc, *host, *certDir) // TODO remove once 1.3 / traffic_ops_golang is deployed to production.
	// }
	if err != nil {
		fmt.Printf("Error creating rules: %v\n", err)
		os.Exit(1)
	}

	jsonRules := grove.RemapRulesToJSON(rules)
	bts := []byte{}
	if *pretty {
		bts, err = json.MarshalIndent(jsonRules, "", "  ")
	} else {
		bts, err = json.Marshal(jsonRules)
	}

	if err != nil {
		fmt.Printf("Error marshalling rules JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s", string(bts))
	os.Exit(0)
}

func createRulesOldAPI(toc *to.Session, host string, certDir string) (grove.RemapRules, error) {
	cachegroupsArr, err := toc.CacheGroups()
	if err != nil {
		fmt.Printf("Error getting Traffic Ops Cachegroups: %v\n", err)
		os.Exit(1)
	}
	cachegroups := makeCachegroupsNameMap(cachegroupsArr)

	serversArr, err := toc.Servers()
	if err != nil {
		fmt.Printf("Error getting Traffic Ops Servers: %v\n", err)
		os.Exit(1)
	}
	servers := makeServersHostnameMap(serversArr)

	hostServer, ok := servers[host]
	if !ok {
		fmt.Printf("Error: host '%v' not in Servers\n", host)
		os.Exit(1)
	}

	deliveryservices, err := toc.DeliveryServicesByServer(hostServer.ID)
	if err != nil {
		fmt.Printf("Error getting Traffic Ops Deliveryservices: %v\n", err)
		os.Exit(1)
	}

	deliveryserviceRegexArr, err := toc.DeliveryServiceRegexes()
	if err != nil {
		fmt.Printf("Error getting Traffic Ops Deliveryservice Regexes: %v\n", err)
		os.Exit(1)
	}
	deliveryserviceRegexes := makeDeliveryserviceRegexMap(deliveryserviceRegexArr)

	cdnsArr, err := toc.CDNs()
	if err != nil {
		fmt.Printf("Error getting Traffic Ops CDNs: %v\n", err)
		os.Exit(1)
	}
	cdns := makeCDNMap(cdnsArr)

	serverParameters, err := toc.Parameters(hostServer.Profile)
	if err != nil {
		fmt.Printf("Error getting Traffic Ops Parameters for host '%v' profile '%v': %v\n", host, hostServer.Profile, err)
		os.Exit(1)
	}

	parents, err := getParents(host, servers, cachegroups)
	if err != nil {
		fmt.Printf("Error getting '%v' parents: %v\n", host, err)
		os.Exit(1)
	}

	sameCDN := func(s tc.Server) bool {
		return s.CDNName == hostServer.CDNName
	}

	serverAvailable := func(s tc.Server) bool {
		status := strings.ToLower(s.Status)
		statuses := AvailableStatuses()
		_, ok := statuses[status]
		return ok
	}

	parents = filterParents(parents, sameCDN)
	parents = filterParents(parents, serverAvailable)

	cdnSSLKeys, err := toc.CDNSSLKeys(hostServer.CDNName)
	if err != nil {
		fmt.Printf("Error getting %v SSL keys: %v\n", hostServer.CDNName, err)
		os.Exit(1)
	}
	dsCerts := makeDSCertMap(cdnSSLKeys)

	return createRulesOld(host, deliveryservices, parents, deliveryserviceRegexes, cdns, serverParameters, dsCerts, certDir)
}

// func createRulesNewAPI(toc *to.Session, host string, certDir string) (grove.RemapRules, error) {
// 	cacheCfg, err := toc.CacheConfig(host)
// 	if err != nil {
// 		fmt.Printf("Error getting Traffic Ops Cache Config: %v\n", err)
// 		os.Exit(1)
// 	}

// 	rules := []grove.RemapRule{}

// 	allowedIPs, err := makeAllowIP(cacheCfg.AllowIP)
// 	if err != nil {
// 		return grove.RemapRules{}, fmt.Errorf("creating allowed IPs: %v", err)
// 	}

// 	cdnSSLKeys, err := toc.CDNSSLKeys(cacheCfg.CDN)
// 	if err != nil {
// 		fmt.Printf("Error getting %v SSL keys: %v\n", cacheCfg.CDN, err)
// 		os.Exit(1)
// 	}
// 	dsCerts := makeDSCertMap(cdnSSLKeys)

// 	weight := DefaultRuleWeight
// 	retryNum := DefaultRetryNum
// 	timeout := DefaultTimeout
// 	parentSelection := DefaultRuleParentSelection

// 	for _, ds := range cacheCfg.DeliveryServices {
// 		protocol := ds.Protocol
// 		queryStringRule, err := getQueryStringRule(ds.QueryStringIgnore)
// 		if err != nil {
// 			return grove.RemapRules{}, fmt.Errorf("getting deliveryservice %v Query String Rule: %v", ds.XMLID, err)
// 		}

// 		protocolStrs := []ProtocolStr{}
// 		switch protocol {
// 		case ProtocolHTTP:
// 			protocolStrs = append(protocolStrs, ProtocolStr{From: "http", To: "http"})
// 		case ProtocolHTTPS:
// 			protocolStrs = append(protocolStrs, ProtocolStr{From: "https", To: "https"})
// 		case ProtocolHTTPAndHTTPS:
// 			protocolStrs = append(protocolStrs, ProtocolStr{From: "http", To: "http"})
// 			protocolStrs = append(protocolStrs, ProtocolStr{From: "https", To: "https"})
// 		case ProtocolHTTPToHTTPS:
// 			protocolStrs = append(protocolStrs, ProtocolStr{From: "http", To: "https"})
// 			protocolStrs = append(protocolStrs, ProtocolStr{From: "https", To: "https"})
// 		}

// 		cert, hasCert := dsCerts[ds.XMLID]
// 		// DEBUG
// 		// if protocol != ProtocolHTTP {
// 		// 	if !hasCert {
// 		// 		fmt.Fprint(os.Stderr, "HTTPS delivery service: "+ds.XMLID+" has no certificate!\n")
// 		// 	} else if err := createCertificateFiles(cert, certDir); err != nil {
// 		// 		fmt.Fprint(os.Stderr, "HTTPS delivery service "+ds.XMLID+" failed to create certificate: "+err.Error()+"\n")
// 		// 	}
// 		// }

// 		dsType := strings.ToLower(ds.Type)
// 		if !strings.HasPrefix(dsType, "http") && !strings.HasPrefix(dsType, "dns") {
// 			fmt.Printf("createRules skipping deliveryservice %v - unknown type %v", ds.XMLID, ds.Type)
// 			continue
// 		}

// 		for _, protocolStr := range protocolStrs {
// 			for _, dsRegex := range ds.Regexes {
// 				rule := grove.RemapRule{}
// 				pattern, patternLiteralRegex := trimLiteralRegex(dsRegex)
// 				rule.Name = fmt.Sprintf("%s.%s.%s.%s", ds.XMLID, protocolStr.From, protocolStr.To, pattern)
// 				rule.From = buildFrom(protocolStr.From, pattern, patternLiteralRegex, host, dsType, cacheCfg.Domain)

// 				if protocolStr.From == "https" && hasCert {
// 					rule.CertificateFile = getCertFileName(cert, certDir)
// 					rule.CertificateKeyFile = getCertKeyFileName(cert, certDir)
// 					// fmt.Fprintf(os.Stderr, "HTTPS delivery service: "+ds.XMLID+" certificate %+v\n", cert)
// 				}

// 				for _, parent := range cacheCfg.Parents {
// 					to, proxyURLStr := buildToNew(parent, protocolStr.To, ds.OriginFQDN, dsType)
// 					proxyURL, err := url.Parse(proxyURLStr)
// 					if err != nil {
// 						return grove.RemapRules{}, fmt.Errorf("error parsing deliveryservice %v parent %v proxy_url: %v", ds.XMLID, parent.Host, proxyURLStr)
// 					}

// 					ruleTo := grove.RemapRuleTo{
// 						RemapRuleToBase: grove.RemapRuleToBase{
// 							URL:      to,
// 							Weight:   &weight,
// 							RetryNum: &retryNum,
// 						},
// 						ProxyURL:   proxyURL,
// 						RetryCodes: DefaultRetryCodes(),
// 						Timeout:    &timeout,
// 					}
// 					rule.To = append(rule.To, ruleTo)
// 					// TODO get from TO?
// 					rule.RetryNum = &retryNum
// 					rule.Timeout = &timeout
// 					rule.RetryCodes = DefaultRetryCodes()
// 					rule.QueryString = queryStringRule
// 					rule.DSCP = ds.DSCP
// 					if err != nil {
// 						return grove.RemapRules{}, err
// 					}
// 					rule.ConnectionClose = DefaultRuleConnectionClose
// 					rule.ParentSelection = &parentSelection
// 				}
// 				rules = append(rules, rule)
// 			}
// 		}
// 	}

// 	remapRules := grove.RemapRules{
// 		Rules:           rules,
// 		RetryCodes:      DefaultRetryCodes(),
// 		Timeout:         &timeout,
// 		ParentSelection: &parentSelection,
// 		Stats:           grove.RemapRulesStats{Allow: allowedIPs},
// 	}

// 	return remapRules, nil
// }

func makeServersHostnameMap(servers []tc.Server) map[string]tc.Server {
	m := map[string]tc.Server{}
	for _, server := range servers {
		m[server.HostName] = server
	}
	return m
}

func makeCachegroupsNameMap(cgs []tc.CacheGroup) map[string]tc.CacheGroup {
	m := map[string]tc.CacheGroup{}
	for _, cg := range cgs {
		m[cg.Name] = cg
	}
	return m
}

func makeDeliveryservicesXMLIDMap(dses []tc.DeliveryService) map[string]tc.DeliveryService {
	m := map[string]tc.DeliveryService{}
	for _, ds := range dses {
		m[ds.XMLID] = ds
	}
	return m
}

func makeDeliveryservicesIDMap(dses []tc.DeliveryService) map[int]tc.DeliveryService {
	m := map[int]tc.DeliveryService{}
	for _, ds := range dses {
		m[ds.ID] = ds
	}
	return m
}

func makeDeliveryserviceRegexMap(dsrs []tc.DeliveryServiceRegexes) map[string][]tc.DeliveryServiceRegex {
	m := map[string][]tc.DeliveryServiceRegex{}
	for _, dsr := range dsrs {
		m[dsr.DSName] = dsr.Regexes
	}
	return m
}

func makeCDNMap(cdns []tc.CDN) map[string]tc.CDN {
	m := map[string]tc.CDN{}
	for _, cdn := range cdns {
		m[cdn.Name] = cdn
	}
	return m
}

func makeDSCertMap(sslKeys []tc.CDNSSLKeys) map[string]tc.CDNSSLKeys {
	m := map[string]tc.CDNSSLKeys{}
	for _, sslkey := range sslKeys {
		m[sslkey.DeliveryService] = sslkey
	}
	return m
}

func getServerDeliveryservices(hostname string, servers map[string]tc.Server, dssrvs []tc.DeliveryServiceServer, dses []tc.DeliveryService) ([]tc.DeliveryService, error) {
	server, ok := servers[hostname]
	if !ok {
		return nil, fmt.Errorf("server %v not found in Traffic Ops Servers", hostname)
	}
	serverID := server.ID
	dsByID := makeDeliveryservicesIDMap(dses)
	serverDses := []tc.DeliveryService{}
	for _, dssrv := range dssrvs {
		if dssrv.Server != serverID {
			continue
		}
		ds, ok := dsByID[dssrv.DeliveryService]
		if !ok {
			return nil, fmt.Errorf("delivery service ID %v not found in Traffic Ops DeliveryServices", dssrv.DeliveryService)
		}
		serverDses = append(serverDses, ds)
	}
	return serverDses, nil
}

func getParents(hostname string, servers map[string]tc.Server, cachegroups map[string]tc.CacheGroup) ([]tc.Server, error) {
	server, ok := servers[hostname]
	if !ok {
		return nil, fmt.Errorf("hostname not found in Servers")
	}

	cachegroup, ok := cachegroups[server.Cachegroup]
	if !ok {
		return nil, fmt.Errorf("server cachegroup '%v' not found in Cachegroups", server.Cachegroup)
	}

	parents := []tc.Server{}
	for _, server := range servers {
		if server.Cachegroup == cachegroup.ParentName {
			parents = append(parents, server)
		}
	}
	return parents, nil
}

func filterParents(parents []tc.Server, include func(tc.Server) bool) []tc.Server {
	newParents := []tc.Server{}
	for _, parent := range parents {
		if include(parent) {
			newParents = append(newParents, parent)
		}
	}
	return newParents
}

const ProtocolHTTP = 0
const ProtocolHTTPS = 1
const ProtocolHTTPAndHTTPS = 2
const ProtocolHTTPToHTTPS = 3

type ProtocolStr struct {
	From string
	To   string
}

// trimLiteralRegex removes the prefix and suffix in .*\.foo\.* delivery service regexes. Traffic Ops Delivery Services have regexes of this form, which aren't really regexes, and the .*\ and \.* need stripped to construct the "to" FQDN. Returns the trimmed string, and whether it was of the form `.*\.foo\.*`
func trimLiteralRegex(s string) (string, bool) {
	prefix := `.*\.`
	suffix := `\..*`
	if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix) {
		return s[len(prefix) : len(s)-len(suffix)], true
	}
	return s, false
}

// buildFrom builds the remap "from" URI prefix. It assumes ttype is a delivery service type HTTP or DNS, behavior is undefined for any other ttype.
func buildFrom(protocol string, pattern string, patternLiteralRegex bool, host string, dsType string, cdnDomain string) string {
	if !patternLiteralRegex {
		return protocol + "://" + pattern
	}

	if isHTTP := strings.HasPrefix(dsType, "http"); isHTTP {
		return protocol + "://" + host + "." + pattern + "." + cdnDomain
	}

	return protocol + "://" + "edge." + pattern + "." + cdnDomain
}

func dsTypeSkipsMid(ttype string) bool {
	ttype = strings.ToLower(ttype)
	if ttype == "http_no_cache" || ttype == "http_live" || ttype == "dns_live" {
		return true
	}
	if strings.Contains(ttype, "live") && !strings.Contains(ttype, "natnl") {
		return true
	}
	return false
}

// buildTo returns the to URL, and the Proxy URL (if any)
func buildTo(parentServer tc.Server, protocol string, originURI string, dsType string) (string, string) {
	// TODO add port?
	to := originURI
	proxy := ""
	if !dsTypeSkipsMid(dsType) {
		proxy = "http://" + parentServer.HostName + "." + parentServer.DomainName + ":" + strconv.Itoa(parentServer.TCPPort)
	}
	return to, proxy
}

// // buildToNew returns the to URL, and the Proxy URL (if any)
// func buildToNew(parent tc.CacheConfigParent, protocol string, originURI string, dsType string) (string, string) {
// 	// TODO add port?
// 	to := originURI
// 	proxy := ""
// 	if !dsTypeSkipsMid(dsType) {
// 		proxy = "http://" + parent.Host + "." + parent.Domain + ":" + strconv.FormatUint(uint64(parent.Port), 10)
// 	}
// 	return to, proxy
// }

const DeliveryServiceQueryStringCacheAndRemap = 0
const DeliveryServiceQueryStringNoCacheRemap = 1
const DeliveryServiceQueryStringNoCacheNoRemap = 2

func getQueryStringRule(dsQstringIgnore int) (grove.QueryStringRule, error) {
	switch dsQstringIgnore {
	case DeliveryServiceQueryStringCacheAndRemap:
		return grove.QueryStringRule{Remap: true, Cache: true}, nil
	case DeliveryServiceQueryStringNoCacheRemap:
		return grove.QueryStringRule{Remap: true, Cache: true}, nil
	case DeliveryServiceQueryStringNoCacheNoRemap:
		return grove.QueryStringRule{Remap: false, Cache: false}, nil
	default:
		return grove.QueryStringRule{}, fmt.Errorf("unknown delivery service qstringIgnore value '%v'", dsQstringIgnore)
	}
}

func DefaultRetryCodes() map[int]struct{} {
	return map[int]struct{}{}
}

const DefaultRuleWeight = 1.0
const DefaultRetryNum = 5
const DefaultTimeout = time.Millisecond * 5000
const DefaultRuleConnectionClose = false
const DefaultRuleParentSelection = grove.ParentSelectionTypeConsistentHash

func getAllowIP(params []tc.Parameter) ([]*net.IPNet, error) {
	ips := []string{}
	for _, param := range params {
		if (param.Name == "allow_ip" || param.Name == "allow_ip6") && param.ConfigFile == "astats.config" {
			ips = append(ips, strings.Split(param.Value, ",")...)
		}
	}
	return makeAllowIP(ips)
}

func makeAllowIP(ips []string) ([]*net.IPNet, error) {
	cidrs := make([]*net.IPNet, len(ips))
	for i, ip := range ips {
		ip = strings.TrimSpace(ip)
		if !strings.Contains(ip, "/") {
			if strings.Contains(ip, ":") {
				ip += "/128"
			} else {
				ip += "/32"
			}
		}
		_, cidrnet, err := net.ParseCIDR(ip)
		if err != nil {
			return nil, fmt.Errorf("error parsing CIDR '%s': %v", ip, err)
		}
		cidrs[i] = cidrnet
	}
	return cidrs, nil
}

func createRulesOld(
	hostname string,
	dses []tc.DeliveryService,
	parents []tc.Server,
	dsRegexes map[string][]tc.DeliveryServiceRegex,
	cdns map[string]tc.CDN,
	hostParams []tc.Parameter,
	dsCerts map[string]tc.CDNSSLKeys,
	certDir string,
) (grove.RemapRules, error) {
	rules := []grove.RemapRule{}
	allowedIPs, err := getAllowIP(hostParams)
	if err != nil {
		return grove.RemapRules{}, fmt.Errorf("getting allowed IPs: %v", err)
	}

	weight := DefaultRuleWeight
	retryNum := DefaultRetryNum
	timeout := DefaultTimeout
	parentSelection := DefaultRuleParentSelection

	for _, ds := range dses {
		protocol := ds.Protocol
		queryStringRule, err := getQueryStringRule(ds.QStringIgnore)
		if err != nil {
			return grove.RemapRules{}, fmt.Errorf("getting deliveryservice %v Query String Rule: %v", ds.XMLID, err)
		}

		cdn, ok := cdns[ds.CDNName]
		if !ok {
			return grove.RemapRules{}, fmt.Errorf("deliveryservice '%v' CDN '%v' not found", ds.XMLID, ds.CDNName)
		}

		protocolStrs := []ProtocolStr{}
		switch protocol {
		case ProtocolHTTP:
			protocolStrs = append(protocolStrs, ProtocolStr{From: "http", To: "http"})
		case ProtocolHTTPS:
			protocolStrs = append(protocolStrs, ProtocolStr{From: "https", To: "https"})
		case ProtocolHTTPAndHTTPS:
			protocolStrs = append(protocolStrs, ProtocolStr{From: "http", To: "http"})
			protocolStrs = append(protocolStrs, ProtocolStr{From: "https", To: "https"})
		case ProtocolHTTPToHTTPS:
			protocolStrs = append(protocolStrs, ProtocolStr{From: "http", To: "https"})
			protocolStrs = append(protocolStrs, ProtocolStr{From: "https", To: "https"})
		}

		cert, hasCert := dsCerts[ds.XMLID]
		if protocol != ProtocolHTTP {
			if !hasCert {
				fmt.Fprint(os.Stderr, "HTTPS delivery service: "+ds.XMLID+" has no certificate!\n")
			} else if err := createCertificateFiles(cert, certDir); err != nil {
				fmt.Fprint(os.Stderr, "HTTPS delivery service "+ds.XMLID+" failed to create certificate: "+err.Error()+"\n")
			}
		}

		dsType := strings.ToLower(ds.Type)
		if !strings.HasPrefix(dsType, "http") && !strings.HasPrefix(dsType, "dns") {
			fmt.Printf("createRules skipping deliveryservice %v - unknown type %v", ds.XMLID, ds.Type)
			continue
		}

		for _, protocolStr := range protocolStrs {
			regexes, ok := dsRegexes[ds.XMLID]
			if !ok {
				return grove.RemapRules{}, fmt.Errorf("deliveryservice '%v' has no regexes", ds.XMLID)
			}

			for _, dsRegex := range regexes {
				rule := grove.RemapRule{}
				pattern, patternLiteralRegex := trimLiteralRegex(dsRegex.Pattern)
				rule.Name = fmt.Sprintf("%s.%s.%s.%s", ds.XMLID, protocolStr.From, protocolStr.To, pattern)
				rule.From = buildFrom(protocolStr.From, pattern, patternLiteralRegex, hostname, dsType, cdn.DomainName)

				if protocolStr.From == "https" && hasCert {
					rule.CertificateFile = getCertFileName(cert, certDir)
					rule.CertificateKeyFile = getCertKeyFileName(cert, certDir)
					// fmt.Fprintf(os.Stderr, "HTTPS delivery service: "+ds.XMLID+" certificate %+v\n", cert)
				}

				for _, parent := range parents {
					to, proxyURLStr := buildTo(parent, protocolStr.To, ds.OrgServerFQDN, dsType)
					proxyURL, err := url.Parse(proxyURLStr)
					if err != nil {
						return grove.RemapRules{}, fmt.Errorf("error parsing deliveryservice %v parent %v proxy_url: %v", ds.XMLID, parent.HostName, proxyURLStr)
					}

					ruleTo := grove.RemapRuleTo{
						RemapRuleToBase: grove.RemapRuleToBase{
							URL:      to,
							Weight:   &weight,
							RetryNum: &retryNum,
						},
						ProxyURL:   proxyURL,
						RetryCodes: DefaultRetryCodes(),
						Timeout:    &timeout,
					}
					rule.To = append(rule.To, ruleTo)
					// TODO get from TO?
					rule.RetryNum = &retryNum
					rule.Timeout = &timeout
					rule.RetryCodes = DefaultRetryCodes()
					rule.QueryString = queryStringRule
					rule.DSCP = ds.DSCP
					if err != nil {
						return grove.RemapRules{}, err
					}
					rule.ConnectionClose = DefaultRuleConnectionClose
					rule.ParentSelection = &parentSelection
				}
				rules = append(rules, rule)
			}
		}
	}

	remapRules := grove.RemapRules{
		Rules:           rules,
		RetryCodes:      DefaultRetryCodes(),
		Timeout:         &timeout,
		ParentSelection: &parentSelection,
		Stats:           grove.RemapRulesStats{Allow: allowedIPs},
	}

	return remapRules, nil
}

func getCertFileName(cert tc.CDNSSLKeys, dir string) string {
	return dir + string(os.PathSeparator) + strings.Replace(cert.Hostname, "*.", "", -1) + ".crt"
}

func getCertKeyFileName(cert tc.CDNSSLKeys, dir string) string {
	return dir + string(os.PathSeparator) + strings.Replace(cert.Hostname, "*.", "", -1) + ".key"
}

func createCertificateFiles(cert tc.CDNSSLKeys, dir string) error {
	certFileName := getCertFileName(cert, dir)
	crt, err := base64.StdEncoding.DecodeString(cert.Certificate.Crt)
	if err != nil {
		return errors.New("base64decoding certificate file " + certFileName + ": " + err.Error())
	}
	if err := ioutil.WriteFile(certFileName, crt, 0644); err != nil {
		return errors.New("writing certificate file " + certFileName + ": " + err.Error())
	}

	keyFileName := getCertKeyFileName(cert, dir)
	key, err := base64.StdEncoding.DecodeString(cert.Certificate.Key)
	if err != nil {
		return errors.New("base64decoding certificate key " + keyFileName + ": " + err.Error())
	}
	if err := ioutil.WriteFile(keyFileName, key, 0644); err != nil {
		return errors.New("writing certificate key file " + keyFileName + ": " + err.Error())
	}
	return nil
}