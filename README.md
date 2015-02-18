RightScale API Command Line Client
==================================

This simple binary executable simplifies performing RightAPI 1.5 and RightAPI 1.6 calls
from the command line, either interactively or in shell scripts.

Synopsis
--------

`rs-api [-flags...] resource_href action parameters...`

Perform an API request on the resource or resource collection named by `resource_href` to
perform the requested `action` with the specified `parameters`.
The command line mimicks the actual [API definition](http://reference.rightscale.com/api1.5)
very closely using the same resource hrefs, the same action names (all lower-case),
and the same parameters.

- `resource_href` is the href of a resource or resource collection to be operated on,
  such as `/api/servers/123456` or `/api/cloud/1/instances/1234345`.
	A few abbreviations are supported as syntactic sugar: the resource type can be used
  for a "global" collection such as `servers` (same as `/api/servers`), and `self` can be
	used as the instance's self_href.
- `action` is one of the actions defined on the resource as named in the API docs, such as
  `index`, `show, `create`, `update`, `delete`, `terminate`, `multi_add`, ...
- `parameters` are the query string parameters as defined in the API docs, such as
  `instance[name]=my instance`, without any query-string encoding (there is no ambiguity
  so rs-api can parse the command line and query-string encode when forming the HTTP
  request

Flags:
- `--host=<hostname:port>` is the hostname (and optional :port suffix) for the RightScale APi endpoint
- `--key=<key>` is the RightScale API key to authenticate
- `--rl10` tells rs-api to proxy through RightLink10 and locate the RL10 port and secret in
  `/var/run/rll-secret`
- `--pretty` pretty-prints the result
- `--x1=<JSONselect>` extracts the single value using the [JSON:select](http://jsonselect.org)
   expression
- `--xm=<JSONselect>` extracts zero, one or multiple values and prints the result as one value per
   line (in _bash_ use something like `clouds=(\`rs-api --xm ...\`)` to get the results into a list
- `--xj=<JSONselect>` is the same as `--xm` but prints the result as a json array
- `--xh=<header> extracts the named header
- `--noRedirect` tells rs-api not to follow any redirects
- `--fetch` tells rs-api to fetch any resource referenced in a response Location header, this
  is helpful to "auto-fetch" a newly created resource

Extracted values are printed on stdout. `--x1` and `--xh` print the result in one line,
`--xm` prints the result as one value per line
(in _bash_ use something like `clouds=($(rs-api --xm ...))` to get the results into a list).
`--xj=` prints the result as a json array.

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
           --x1 '.public_ip_addresses' /api/clouds/1/instances/LAB4OFL7I82E show
["54.147.25.88"]
```

- Find an instance's resource_uid:
```
./rs-api --host us-3.rightscale.com --key 1234567890 \
           --x1 '.resource_uid' /api/clouds/1/instances/LAB4OFL7I82E show
"i-4e9a80b5"
```

- Find an instance's server href:
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --x1 'object:has(.rel:val("parent")).href' /api/clouds/1/instances/LAB4OFL7I82E show
"/api/servers/994838003"
```

- Find an instance's cloud type:
```
cloud=$(./rs-api --host us-3.rightscale.com --key 1234567890 \
        --x1 'object:has(.rel:val("cloud")).href' /api/clouds/1/instances/LAB4OFL7I82E show)

- Find the hrefs of all clouds of type amazon:
```
./rs-api --host us-3.rightscale.com --key 1234567890 \
clouds index 'filter[]=cloud_type==amazon'
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --xm 'object:has(.rel:val("self")).href' clouds index 'filter[]=cloud_type==amazon'
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
"rel":"datacenters"}, ... ]` and the json:select expression says
something like: find an _object_ (json hash) that has a _rel_ child/field whose value is _self_
and then extract the value of the _href_ child/field. The _object_ here matches the
`{"href":"/api/clouds/7","rel":"self"}` hash.

Illustrating the difference between `--x1`, `--xm`, and `--xj`:
- `--x1` produces: `rs-api: error: Multiple values selected, result was:
  <<[{"cloud_type":"amazon","descr... >>` with a non-zero exit code (it prints the raw json
	for troubleshootingpurposes).
- `--xm` produces: `"/api/clouds/1" "/api/clouds/3" "/api/clouds/4" "/api/clouds/5"
  "/api/clouds/6" "/api/clouds/7" "/api/clouds/2" "/api/clouds/8" "/api/clouds/9"` and can be used
	in bash as `cloud_hrefs=(`./rs-api ...`)
- `--xj` produces: `["/api/clouds/1", "/api/clouds/3", "/api/clouds/4", "/api/clouds/5",
   "/api/clouds/6", "/api/clouds/7", "/api/clouds/2", "/api/clouds/8", "/api/clouds/9"]`

- Find a running or stopped instance by public IP address in AWS us-east (cloud #1):
```
$ ./rs-api --host us-3.rightscale.com --key 1234567890 \
           --xm 'object:has(.rel:val("self")).href' /api/clouds/1/instances index \
           'filter[]=public_ip_address==54.147.25.88' 'filter[]=state<>terminated' \
           'filter[]=state<>decommissioning' 'filter[]=state<>terminating' \
           'filter[]=state<>stopping' 'filter[]=state<>provisioned' 'filter[]=state<>failed'
"/api/clouds/1/instances/LAB4OFL7I82E"
```



