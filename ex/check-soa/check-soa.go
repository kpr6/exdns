package main

import (
	"errors"
	"fmt"
	"github.com/miekg/dns"
	"os"
	"strings"
	"time"
)

const (
	TIMEOUT time.Duration = 5 // seconds
)

var (
	localm *dns.Msg
	localc *dns.Client
	conf   *dns.ClientConfig
)

func localQuery(qname string, qtype uint16) (r *dns.Msg, err error) {
	localm.SetQuestion(qname, qtype)
	for i := range conf.Servers {
		server := conf.Servers[i]
		r, err := localc.Exchange(localm, server+":"+conf.Port)
		if r == nil || r.Rcode == dns.RcodeNameError || r.Rcode == dns.RcodeSuccess {
			return r, err
		}
	}
	return nil, errors.New("No name server to answer the question")
}

func main() {
	var err error
	if len(os.Args) != 2 {
		fmt.Printf("%s ZONE\n", os.Args[0])
		os.Exit(1)
	}
	conf, err = dns.ClientConfigFromFile("/etc/resolv.conf")
	if conf == nil {
		fmt.Printf("Cannot initialize the local resolver: %s\n", err)
		os.Exit(1)
	}
	localm = new(dns.Msg)
	localm.MsgHdr.RecursionDesired = true
	localm.Question = make([]dns.Question, 1)
	localc = new(dns.Client)
	localc.ReadTimeout = TIMEOUT * 1e9
	r, err := localQuery(dns.Fqdn(os.Args[1]), dns.TypeNS)
	if r == nil {
		fmt.Printf("Cannot retrieve the list of name servers for %s: %s\n", dns.Fqdn(os.Args[1]), err)
		os.Exit(1)
	}
	if r.Rcode == dns.RcodeNameError {
		fmt.Printf("No such domain %s\n", dns.Fqdn(os.Args[1]))
		os.Exit(1)
	}
	m := new(dns.Msg)
	m.MsgHdr.RecursionDesired = false
	m.Question = make([]dns.Question, 1)
	c := new(dns.Client)
	c.ReadTimeout = TIMEOUT * 1e9
	success := true
	numNS := 0
	for _, ans := range r.Answer {
		switch ans.(type) {
		case *dns.RR_NS:
			nameserver := ans.(*dns.RR_NS).Ns
			numNS += 1
			ips := make([]string, 0)
			fmt.Printf("%s : ", nameserver)
			ra, err := localQuery(nameserver, dns.TypeA)
			if ra == nil {
				fmt.Printf("Error getting the IPv4 address of %s: %s\n", nameserver, err)
				os.Exit(1)
			}
			if ra.Rcode != dns.RcodeSuccess {
				fmt.Printf("Error getting the IPv4 address of %s: %s\n", nameserver, dns.Rcode_str[ra.Rcode])
				os.Exit(1)
			}
			for _, ansa := range ra.Answer {
				switch ansa.(type) {
				case *dns.RR_A:
					ips = append(ips, ansa.(*dns.RR_A).A.String())
				}
			}
			raaaa, err := localQuery(nameserver, dns.TypeAAAA)
			if raaaa == nil {
				fmt.Printf("Error getting the IPv6 address of %s: %s\n", nameserver, err)
				os.Exit(1)
			}
			if raaaa.Rcode != dns.RcodeSuccess {
				fmt.Printf("Error getting the IPv6 address of %s: %s\n", nameserver, dns.Rcode_str[raaaa.Rcode])
				os.Exit(1)
			}
			for _, ansaaaa := range raaaa.Answer {
				switch ansaaaa.(type) {
				case *dns.RR_AAAA:
					ips = append(ips, ansaaaa.(*dns.RR_AAAA).AAAA.String())
				}
			}
			if len(ips) == 0 {
				success = false
				fmt.Printf("No IP address for this server")
			}
			for _, ip := range ips {
				m.Question[0] = dns.Question{dns.Fqdn(os.Args[1]), dns.TypeSOA, dns.ClassINET}
				nsAddressPort := ""
				if strings.ContainsAny(":", ip) {
					/* IPv6 address */
					nsAddressPort = "[" + ip + "]:53"
				} else {
					nsAddressPort = ip + ":53"
				}
				soa, err := c.Exchange(m, nsAddressPort)
				// TODO: retry if timeout? Otherwise, one lost UDP packet and it is the end
				if soa == nil {
					success = false
					fmt.Printf("%s (%s) ", ip, err)
				} else {
					if soa.Rcode != dns.RcodeSuccess {
						success = false
						fmt.Printf("%s (%s) ", ips, dns.Rcode_str[soa.Rcode])
					} else {
						if len(soa.Answer) == 0 { // May happen if the server is a recursor, not authoritative, since we query with RD=0 
							success = false
							fmt.Printf("%s (0 answer) ", ip)
						} else {
							rsoa := soa.Answer[0]
							switch rsoa.(type) {
							case *dns.RR_SOA:
								if soa.MsgHdr.Authoritative {
									// TODO: test if all name servers have the same serial ?
									fmt.Printf("%s (%d) ", ips, rsoa.(*dns.RR_SOA).Serial)
								} else {
									success = false
									fmt.Printf("%s (not authoritative) ", ips)
								}
							}
						}
					}
				}
			}
			fmt.Printf("\n")
		}
	}
	if numNS == 0 {
		fmt.Printf("No NS records for \"%s\". It is probably a CNAME to a domain but not a zone\n", dns.Fqdn(os.Args[1]))
		os.Exit(1)
	}
	if success {
		os.Exit(0)
	}
	os.Exit(1)
}
