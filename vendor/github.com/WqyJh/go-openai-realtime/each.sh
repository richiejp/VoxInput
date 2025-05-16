#!/bin/bash

mods=(
    .
    ./contrib/ws-gorilla
)

for mod in "${mods[@]}"; do
    (cd "$mod" && "$@")
done
