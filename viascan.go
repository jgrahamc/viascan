// viascan is used to test one of more origin web servers to see if
// they give different results when asking for gzipped content when an
// HTTP Via header is or is not present.
//
// It expects to receive one or more lines on stdin that consist of
// comma separated entries representing an HTTP Host header value and
// the name of an origin web server to which to send an HTTP
// request. For example,
//
//      echo "www.cloudflare.com,cloudflare.com" | ./viascan
//
// would connect to cloudflare.com and do a GET for / with the Host
// header set to www.cloudflare.com. The origin can be an IP address.
//
// viascan outputs one comma-separated line per input line.
//
// For example, the above might output:
//
// cloudflare.com,www.cloudflare.com,t,t,t,2038,2038,gzip,gzip,
// cloudflare-nginx,cloudflare-nginx
//
// Breaking that down:
//
// cloudflare.com,           Origin server contacted
// www.cloudflare.com,       Host header sent
// t,                        t if the origin server name resolved
// t,                        t if a GET / with no Via header worked
// t,                        t if a GET / with a Via header worked
// 2038,                     Size in bytes of the response to GET / with no Via
// 2038,                     Size in bytes of the response to GET / with Via
// gzip,                     Content-Encoding in response with no Via header
// gzip,                     Content-Encoding in response with a Via header
// cloudflare-nginx,         Server in response with no Via header
// cloudflare-nginx          Server in response with a Via header

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/bogdanovich/dns_resolver"
)

var resolverName string
var dump *bool

// tri captures a tri-state. The value of yesno is true only is ran is
// true
type tri struct {
	ran   bool
	yesno bool
}

func (t tri) String() string {
	switch {
	case !t.ran:
		return "-"
	case t.yesno:
		return "t"
	case !t.yesno:
		return "f"
	}

	// Should not be reached ever

	return "!"
}

// site is a web site identified by its DNS name along with the state
// of various tests performed on the site.
type site struct {
	host   string // Host header that needs to be set
	origin string // DNS name of the web site

	resolves tri // Whether the name resolves
	noVia    tri // Whether request without Via header works
	via      tri // Whether request with Via header works

	noViaSize int // Size of the body returned with no Via header
	viaSize   int // Size of the body returned with a Via header

	noViaEncoding string // Content-Encoding header with no Via header
	viaEncoding   string // Content-Encoding header with Via header

	noViaServer string // Server header with no Via header
	viaServer   string // Server header with Via header
}

// test tests a site and looks at Via support
func (s *site) test(l *os.File) {
	resolver := dns_resolver.New([]string{resolverName})

	// Check that the origin server resolves

	s.resolves.ran = true
	name := s.origin
	if net.ParseIP(name) == nil {
		_, err := resolver.LookupHost(name)
		if err != nil {
			s.logf(l, "Error resolving name: %s", err)
			s.resolves.yesno = false
			return
		}
	}
	s.resolves.yesno = true

	protocol := "http://"

	// Note: we disable compression in the http.Transport so that the
	// Go library does not add the Accept-Encoding and does not do
	// transparent decompression.
	//
	// The Accept-Encoding header is added to the request which means
	// that we'll potentially get gzipped content in return.

	transport := &http.Transport{}
	transport.DisableCompression = true

	// Custom dialer is needed to use special DNS resolver so that the
	// default resolver can be overriden

	transport.Dial = func(network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		if net.ParseIP(host) != nil {
			return net.Dial(network, address)
		}

		ips, err := resolver.LookupHost(host)
		if err != nil {
			return nil, err
		}

		if len(ips) == 0 {
			return nil, fmt.Errorf("Failed to get any IPs for %s", address)
		}

		return net.Dial(network, net.JoinHostPort(ips[0].String(), port))
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequest("GET", protocol+name, nil)

	req.Header.Set("Accept-Encoding", "gzip,deflate")
	req.Header.Set("Host", s.host)

	s.noVia.ran = true
	if *dump {
		fmt.Printf("%#v\n", req)
	}
	respNoVia, err := client.Do(req)
	if *dump {
		fmt.Printf("%#v\n", respNoVia)
	}
	if err != nil {
		s.logf(l, "HTTP request %#v failed: %s", req, err)
		return
	}
	s.noVia.yesno = true
	sizeNoVia := 0
	if respNoVia != nil && respNoVia.Body != nil {
		b, _ := ioutil.ReadAll(respNoVia.Body)
		sizeNoVia = len(b)
		respNoVia.Body.Close()
	}
	s.noViaSize = sizeNoVia
	s.noViaEncoding = respNoVia.Header.Get("Content-Encoding")
	s.noViaServer = respNoVia.Header.Get("Server")
	transport.CloseIdleConnections()

	// Now add the Via header to the same request and repeate

	req.Header.Set("Via", "viascan 1.0")

	s.via.ran = true
	if *dump {
		fmt.Printf("%#v\n", req)
	}
	respVia, err := client.Do(req)
	if *dump {
		fmt.Printf("%#v\n", respVia)
	}
	if err != nil {
		s.logf(l, "HTTP request %#v failed: %s", req, err)
		return
	}
	s.via.yesno = true
	sizeVia := 0
	if respVia != nil && respVia.Body != nil {
		b, _ := ioutil.ReadAll(respVia.Body)
		sizeVia = len(b)
		respVia.Body.Close()
	}
	s.viaSize = sizeVia
	s.viaEncoding = respVia.Header.Get("Content-Encoding")
	s.viaServer = respVia.Header.Get("Server")
	transport.CloseIdleConnections()
}

// logf writes to the log file prefixing with the origin being logged
func (s *site) logf(f *os.File, format string, a ...interface{}) {
	if f != nil {
		fmt.Fprintf(f, fmt.Sprintf(s.origin+": "+format+"\n", a...))
	}
}

// fields returns the list of fields that String() will return for a
// site
func (s *site) fields() string {
	return "origin,host,resolves,noVia,via,noViaSize,viaSize,noViaEncoding,viaEncoding,noViaServer,viaServer"
}

func (s *site) String() string {
	return fmt.Sprintf("%s,%s,%s,%s,%s,%d,%d,%s,%s,%s,%s", s.origin, s.host,
		s.resolves, s.noVia, s.via, s.noViaSize, s.viaSize, s.noViaEncoding,
		s.viaEncoding, s.noViaServer, s.viaServer)
}

var wg sync.WaitGroup

func worker(work, result chan *site, l *os.File) {
	for s := range work {
		s.test(l)
		result <- s
	}
	wg.Done()
}

func writer(result chan *site, stop chan struct{}, fields bool) {
	first := true
	for s := range result {
		if fields && first {
			fmt.Printf("%s\n", s.fields())
			first = false
		}

		fmt.Printf("%s\n", s)
	}
	close(stop)
}

func main() {
	resolver := flag.String("resolver", "127.0.0.1", "DNS resolver address")
	dump = flag.Bool("dump", false, "Dump requests and responses for debugging")
	fields := flag.Bool("fields", false,
		"If set outputs a header line containing field names")
	workers := flag.Int("workers", 10, "Number of concurrent workers")
	log := flag.String("log", "", "File to write log information to")
	flag.Parse()

	resolverName = *resolver

	if *workers < 1 {
		fmt.Printf("-workers must be a positive number\n")
		return
	}

	var l *os.File
	var err error
	if *log != "" {
		if l, err = os.Create(*log); err != nil {
			fmt.Printf("Failed to create log file %s: %s\n", *log, err)
			return
		}
		defer l.Close()
	}

	work := make(chan *site)
	result := make(chan *site)
	stop := make(chan struct{})

	go writer(result, stop, *fields)

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go worker(work, result, l)
	}

	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		parts := strings.Split(scan.Text(), ",")
		if len(parts) != 2 {
			fmt.Printf("Bad line: %s\n", scan.Text())
		} else {
			work <- &site{host: parts[0], origin: parts[1]}
		}
	}

	close(work)
	wg.Wait()
	close(result)
	<-stop

	if scan.Err() != nil {
		fmt.Printf("Error reading input: %s\n", scan.Err())
		return
	}
}
