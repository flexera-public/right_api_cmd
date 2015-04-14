RightScale API Command Line Client
==================================

This simple binary executable simplifies performing RightAPI 1.5 calls
from the command line, either interactively or in shell scripts.

- Master: [![Build Status](https://travis-ci.org/rightscale/right_api_cmd.svg?branch=master)](https://travis-ci.org/rightscale/right_api_cmd)
  ![Code Coverage](https://s3.amazonaws.com/rs-code-coverage/right_api_cmd/cc_badge_master.svg)

Try [rsc](https://github.com/rightscale/rsc) first
-------

Please give [rsc](https://github.com/rightscale/rsc) a try over right_api_cmd.
This command line client is very simple and just passes what's on the command line through to the
web server. It's great if you want to do some non-standard stuff or you want to use the source
code to hack something up. If you're "just" wanting to issue API requests to one of the
RightScale APIs please use [rsc](https://github.com/rightscale/rsc) instead, which has
virtually the same command line but includes built-in help and supports all RightScale APIs.

Synopsis
--------

`rs-api [-flags...] action resource_href parameters...`

Perform an API request on the resource or resource collection named by `resource_href` to
perform the requested `action` with the specified `parameters`.
The command line mimicks the actual [API definition](http://reference.rightscale.com/api1.5)
very closely using the same resource hrefs, the same action names (all lower-case),
and the same parameters.

- `action` is one of the actions defined on the resource as named in the API docs, such as
  `index`, `show`, `create`, `update`, `delete`, `terminate`, `multi_add`, ...
- `resource_href` is the href of a resource or resource collection to be operated on,
  such as `/api/servers/123456` or `/api/cloud/1/instances/1234345`.
  A few abbreviations are supported as syntactic sugar: the resource type can be used
  for a "global" collection such as `servers` (same as `/api/servers`), and `self` can be
  used as the instance's self_href (the latter only when using `--rl10` authentication.
- `parameters` are the query string parameters as defined in the API docs, such as
  `instance[name]=my instance`, without any query-string encoding (there is no ambiguity
  so rs-api can parse the command line and query-string encode when forming the HTTP
  request

Flags:
- `--host=<hostname:port>` is the hostname (and optional :port suffix) for the RightScale API endpoint
- `--key=<key>` is the RightScale API key to authenticate
- `--rl10` tells rs-api to proxy through RightLink10 and locate the RL10 port and secret in
  `/var/run/rightlink/secret`
- `--pretty` pretty-prints the result
- `--x1=<JSONselect>` extracts the single value using the [JSON:select](http://jsonselect.org)
   expression
- `--xm=<JSONselect>` extracts zero, one or multiple values and prints the result as one value per
   line (in _bash_ use something like `clouds=(\`rs-api --xm ...\`)` to get the results into a list
- `--xj=<JSONselect>` is the same as `--xm` but prints the result as a json array
- `--xh=<header>` extracts the named header

Extracted values are printed on stdout. `--x1` and `--xh` print the result in one line,
`--xm` prints the result as one value per line
(in _bash_ use something like `clouds=($(rs-api --xm ...))` to get the results into a list).
`--xj` prints the result as a json array.

If `--host` or `--key` are not specified, and `--rl10` is also not specified (i.e., rs-api is
asked to contact the RS platform directly) either of these values can be read from the
environment variables `RS_api_hostname` respectively `RS_api_key`.
However, if `--rl10` is specified the environment variables are not consulted but
`/var/run/rightlink/secret` is.

Exit codes:
- 0 = all OK
- 1 = an error occurred

(Is it worth implementing the following more detailed exit codes?
- 1 = 401 authorization required
- 2 = 4XX other client error
- 3 = 403 permission denied
- 4 = 404 not found
- 5 = 5XX server side error
- 6 = Extraction for --x1 does not have exactly one item
)

Examples
--------

- Find instance's public IP addresses
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --x1 '.public_ip_addresses' show /api/clouds/1/instances/LAB4OFL7I82E
["54.147.25.88"]
```

- Find an instance's resource_uid:
```
./rs-api --host us-3.rightscale.com --key 1234567890 \
           --x1 '.resource_uid' show /api/clouds/1/instances/LAB4OFL7I82E
"i-4e9a80b5"
```

- Find an instance's server href:
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --x1 'object:has(.rel:val("parent")).href' \
           show /api/clouds/1/instances/LAB4OFL7I82E
"/api/servers/994838003"
```

- Find an instance's cloud type:
```
cloud=$(./rs-api --host us-3.rightscale.com --key 1234567890 \
        --x1 'object:has(.rel:val("cloud")).href' show /api/clouds/1/instances/LAB4OFL7I82E)
./rs-api --host us-3.rightscale.com --key 1234567890 \
         --x1 .cloud_type show $cloud
```

- Find the hrefs of all clouds of type amazon:
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --xm 'object:has(.rel:val("self")).href' index clouds 'filter[]=cloud_type==amazon'
"/api/clouds/1"
"/api/clouds/3"
"/api/clouds/4"
"/api/clouds/5"
"/api/clouds/6"
"/api/clouds/7"
"/api/clouds/2"
"/api/clouds/8"
"/api/clouds/9"
```
Note: the match `object:has(.rel:val("self")).href` serves to extract the hrefs from the _self_
links. The returned json for each cloud includes
`"links":[ {"href":"/api/clouds/7", "rel":"self"}, {"href":"/api/clouds/7/datacenters",
"rel":"datacenters"}, ... ]` and the json:select expression says:
find an _object_ (json hash) that has a _rel_ child/field whose value is _self_
and then extract the value of the _href_ child/field. The _object_ here matches the
`{"href":"/api/clouds/7","rel":"self"}` hash.

Illustrating the difference between `--x1`, `--xm`, and `--xj`:
- `--x1` produces: `rs-api: error: Multiple values selected, result was:
  <<[{"cloud_type":"amazon","descr... >>` with a non-zero exit code (it prints the raw json
	for troubleshooting purposes).
- `--xm` produces: `"/api/clouds/1" "/api/clouds/3" "/api/clouds/4" "/api/clouds/5"
  "/api/clouds/6" "/api/clouds/7" "/api/clouds/2" "/api/clouds/8" "/api/clouds/9"` and can be used
	in bash as `cloud_hrefs=$(./rs-api ...)`
- `--xj` produces: `["/api/clouds/1", "/api/clouds/3", "/api/clouds/4", "/api/clouds/5",
   "/api/clouds/6", "/api/clouds/7", "/api/clouds/2", "/api/clouds/8", "/api/clouds/9"]`

- Find a running or stopped instance by public IP address in AWS us-east (cloud #1):
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --xm 'object:has(.rel:val("self")).href' index /api/clouds/1/instances \
           'filter[]=public_ip_address==54.147.25.88' 'filter[]=state<>terminated' \
           'filter[]=state<>decommissioning' 'filter[]=state<>terminating' \
           'filter[]=state<>stopping' 'filter[]=state<>provisioned' 'filter[]=state<>failed'
"/api/clouds/1/instances/LAB4OFL7I82E"
```

