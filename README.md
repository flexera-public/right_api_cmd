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
- `-host=<hostname:port>` is the hostname (and optional :port suffix) for the RightScale APi endpoint
- `-key=<key>` is the RightScale API key to authenticate
- `-rl10` tells rs-api to proxy through RightLink10 and locate the RL10 port and secret in
  `/var/run/rll-secret`
- `-pretty` pretty-prints the result
- `-x1=<JSONselect>` extracts the single value using the [JSON:select](http://jsonselect.org) expression
- `-xm=<JSONselect>` extracts zero, one or multiple values
- `-xl=<JSONselect>` extracts the href of a link
- `-xh=<header> extracts the named header
- `-noRedirect` tells rs-api not to follow any redirects
- `-fetch` tells rs-api to fetch any resource referenced in a response Location header, this
  is helpful to "auto-fetch" a newly created resource

Extracted values are printed on stdout in json, if a single -x option is provided then
just the extracted value is printed, if multiple options are provided then a JSON hash is
printed with the json paths and/or header field names as keys and the extracted values as
values.

Exit codes:
- 0 = all OK
- 1 = 401 authorization required
- 2 = 4XX other client error
- 3 = 403 permission denied
- 4 = 404 not found
- 5 = 5XX server side error
