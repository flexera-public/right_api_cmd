RightScale API Command Line Client
==================================

This simple binary executable simplifies performing RightAPI 1.5 and RightAPI 1.6 calls
from the command line, either interactively or in shell scripts.

Synopsis
--------

`rs-api [-flags...] resource_href action parameters...`

Where:
- `resource_href` is either the href of a resource or resource collection to be operated on,
  such as `/api/servers/123456` or `/api/cloud/1/instances/1234345`, or it is an abbreviation
  for a "global" collection such as `servers` (a short-hand for `/api/servers`)
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
- `-x=<json path>` extracts the named path from the json, see below for more info
- `-hx=<header> extracts the named header
- `-noRedirect` tells rs-api not to follow any redirects
- `-fetch` tells rs-api to fetch any resource referenced in a response Location header, this
  is helpful to "auto-fetch" a newly created resource

Extracted values are printed on stdout in json, if a single -x or -hx option is provided then
just the extracted value is printed, if multiple options are provided then a JSON hash is
printed with the json paths and/or header field names as keys and the extracted values as
values.

Exit codes:
- 0 = all OK
- 1 = 401 authorization required
- 2 = 5XX server side error
- 3 = 403 permission denied
- 4 = 404 not found
- 5 = 4XX other client error
