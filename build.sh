#!/bin/bash

# exit immediately if any error occurred
set -e

echo "===== build start ====="

GO=go

cd $(dirname $0)

export GOPATH=$(pwd)

OUTDIR=demo

echo "GOPATH=$GOPATH"

if [ ! -e "$OUTDIR" ]; then
    mkdir "$OUTDIR"
    echo "mkdir $OUTDIR"
fi

cp "$GOPATH/src/democonf/democonf.json" "$OUTDIR"
echo "cp $GOPATH/src/democonf/democonf.json $OUTDIR" 

TARGETS=("alice" "bob" "charlie" "dave" "fred")

for target in ${TARGETS[@]}; do
    printf "==== %7s build start ====\n" "$target"
    cd "$GOPATH/src/$target"
    $GO build -o "../../$OUTDIR/$target" -v
    if [ -e html ]; then
        cp -r html "../../$OUTDIR"
        echo "cp -r html ../../$OUTDIR"
    fi
    printf "==== %7s build end ====\n" "$target"
done

echo "===== buid end ===="

