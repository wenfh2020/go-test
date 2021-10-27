#!/bin/bash

test() {
    sleep 3
    ./main --cnt $1
}

array=(1000 3000 5000 8000 10000 15000 20000 25000)

for x in ${array[*]}; do
    test $x
done
