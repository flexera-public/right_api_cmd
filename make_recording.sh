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

./rs-api ${ARGS[@]} --x1 '*:has(.name:val("EC2 us-east-1")) .name' index clouds
./rs-api ${ARGS[@]} --xm '*:has(.name:val("EC2 us-east-1")) .name' index clouds
./rs-api ${ARGS[@]} --xj '*:has(.name:val("EC2 us-east-1")) .name' index clouds

./rs-api ${ARGS[@]} --xm .local_disks index /api/clouds/1/instance_types

./rs-api ${ARGS[@]} --x1 :root index /api/clouds/3/volume_snapshots \
	'filter[]=resource_uid==snap-00828462'
./rs-api ${ARGS[@]} --xm :root index /api/clouds/3/volume_snapshots \
	'filter[]=resource_uid==snap-00828462'
./rs-api ${ARGS[@]} --xj :root index /api/clouds/3/volume_snapshots \
	'filter[]=resource_uid==snap-00828462'

names=`./rs-api ${ARGS[@]} --xm '*:has(.cloud_type:val("amazon")) .name' index clouds`
declare -a "names=($names)" # magic
set | egrep '^names'
[[ ${#names[@]} = 9 ]] || exit 1
./rs-api ${ARGS[@]} --xj '*:has(.cloud_type:val("amazon")) .name' index clouds

./rs-api ${ARGS[@]} --x1 'object:has(.name:val("rsc-test"))' index deployments
href=`./rs-api ${ARGS[@]} --xh location create deployments \
	'deployment[name]=rsc-test' \
	'deployment[description]=expendable deployment used to test rsc'`
declare -a "href=($href)"
./rs-api ${ARGS[@]} destroy $href

# find existing deployment and delete it
deployment=`./rs-api ${ARGS[@]} \
	--x1 ':has(.rel:val("self")).href' \
	index deployments \
	'filter[]=name==rsc-test'`
echo "deployment: $deployment"
if [[ -n "$deployment" ]]; then
	./rs-api ${ARGS[@]} destroy $deployment
fi

# create a deployment with too few params -> error
./rs-api ${ARGS[@]} --xh location create deployments

# create a deployment to launch an instance in
deployment_href=`./rs-api ${ARGS[@]} --xh location create deployments 'deployment[name]=rsc-test'`
echo "deployment_href: $deployment_href"

# get the href of the EC2 us-east-1 cloud
cloud_href=`./rs-api ${ARGS[@]} \
	--x1 '*:has(.name:val("EC2 us-east-1")) :has(.rel:val("self")).href' \
	index clouds`
echo "cloud_href: $cloud_href"

# locate the image to launch
image_href=`./rs-api ${ARGS[@]} \
	--x1 ':has(.rel:val("self")).href' \
	index $cloud_href/images \
	'filter[]=resource_uid==ami-6089d208'`
echo "image_href: $image_href"

# locate an instance type
inst_type_href=`./rs-api ${ARGS[@]} \
	--x1 ':has(.rel:val("self")).href' \
	index $cloud_href/instance_types \
	'filter[]=name==m3.medium'`
echo "inst_type_href: $inst_type_href"

# launch the instance
instance_href=`./rs-api ${ARGS[@]} \
	--xh location\
	create $cloud_href/instances \
	"instance[image_href]=$image_href" \
	"instance[instance_type_href]=$inst_type_href" \
	"instance[name]=rsc-test"`
echo "instance_href: $instance_href"

# wait for it to be running
while true; do
	state=`./rs-api ${ARGS[@]} \
		--xm '.state' \
		show $instance_href`
	echo state: $state
	if [[ "$state" != pending ]]; then break; fi
	sleep 60
done

./rs-api ${ARGS[@]} show $instance_href
./rs-api ${ARGS[@]} --x1 .locked show $instance_href
./rs-api ${ARGS[@]} --xm .locked show $instance_href
./rs-api ${ARGS[@]} --xj .locked show $instance_href

# find server template
st_href=`./rs-api ${ARGS[@]} \
	--x1 ':has(.rel:val("self")).href' \
	index server_templates \
	'filter[]=name==Rightlink 10.0.rc4 Linux Base' \
	'filter[]=revision==0'`
echo st_href: $st_href

# terminating instance
./rs-api ${ARGS[@]} terminate $instance_href
./rs-api ${ARGS[@]} destroy $deployment_href



