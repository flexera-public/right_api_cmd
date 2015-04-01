#! /bin/bash
if [[ -z "$RS_KEY" ]]; then
  echo "Please set RS_KEY to your API key"
  exit 1
fi

rm -f recording-new.json
ARGS=(--host us-3.rightscale.com --key $RS_KEY --record recording-new.json)

set -x
./rs-api ${ARGS[@]} index clouds
./rs-api ${ARGS[@]} index /api/clouds
./rs-api ${ARGS[@]} show /api/clouds/6
./rs-api ${ARGS[@]} --x1 ".cloud_type" show /api/clouds/6
./rs-api ${ARGS[@]} --xm ".cloud_type" index clouds
./rs-api ${ARGS[@]} --xj ".cloud_type" index clouds

./rs-api ${ARGS[@]} --x1 'object:has(.name:val("rsc-test"))' index deployments
href=`./rs-api ${ARGS[@]} --xh location create deployments 'deployment[name]=rsc-test'`
./rs-api ${ARGS[@]} delete $href
