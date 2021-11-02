#!/bin/sh
# rsync code from mac to linux.
work_path=$(dirname $0)
cd $work_path

src=~/go/src/go-test
# dst=root@lu14:/root/go/src
dst=root@lu20:/root/go/src
echo "$src --> $dst"

# only rsync *.go files.
rsync -ravz --exclude=".git/" --include="*.go" --include="*/" --exclude="*" $src $dst
