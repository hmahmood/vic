#!/bin/bash
# Copyright 2017 VMware, Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# exit on failure and configure debug, include util functions
set -e && [ -n "$DEBUG" ] && set -x
DIR=$(dirname $(readlink -f "$0"))

function usage() {
    echo "Usage: $0 -o outputfile -b bin_dir" 1>&2
    exit 1
}

while getopts "o:b:" flag
do
    case $flag in

        o)
            # Required. output file
            outfile="$OPTARG"
            ;;

        b)
            # required. binary directory
            bin_dir="$OPTARG"
            ;;

        *)
            usage
            ;;
    esac
done

shift $((OPTIND-1))

# check there were no extra args and the required ones are set
if [ ! -z "$*" -o -z "$outfile" -o -z $bin_dir ]; then
    usage
fi


TEMP_DIR=$(mktemp -d)

mkdir -p $(dirname $outfile)
cp LICENSE $TEMP_DIR
cp doc/bundle/README $TEMP_DIR
cp $bin_dir/vic-machine* $TEMP_DIR
cp $bin_dir/appliance.iso $TEMP_DIR
cp $bin_dir/bootstrap.iso $TEMP_DIR
tar czvf $outfile -C $TEMP_DIR .
