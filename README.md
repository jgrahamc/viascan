# viascan

Scan web servers to see if the change compression behaviour when the
HTTP Via header is added.

# Usage

viascan is used to test one of more origin web servers to see if they
give different results when asking for gzipped content when an HTTP
Via header is or is not present.

It expects to receive one or more lines on stdin that consist of comma
separated entries representing an HTTP Host header value and the name
of an origin web server to which to send an HTTP request. For example,

     echo "www.cloudflare.com,cloudflare.com" | ./viascan

would connect to cloudflare.com and do a GET for / with the Host
header set to www.cloudflare.com. The origin can be an IP address.

viascan outputs one comma-separated line per input line.

For example, the above might output:

     cloudflare.com,www.cloudflare.com,t,t,t,2038,2038,gzip,gzip,
     cloudflare-nginx,cloudflare-nginx

Breaking that down:

`cloudflare.com,` Origin server contacted

`www.cloudflare.com,` Host header sent

`t,` t if the origin server name resolved

`t,` t if a GET / with no Via header worked

`t,` t if a GET / with a Via header worked

`2038,` Size in bytes of the response to GET / with no Via

`2038,` Size in bytes of the response to GET / with Via

`gzip,` Content-Encoding in response with no Via header

`gzip,` Content-Encoding in response with a Via header

`cloudflare-nginx,` Server in response with no Via header

`cloudflare-nginx` Server in response with a Via header

# Options

`-dump` Dump requests and responses for debugging

`-fields` If set outputs a header line containing field names
		
`-log` File to write log information to
		
`-resolver` DNS resolver address (default 127.0.0.1)

`-workers` Number of concurrent workers (default 10)
