#!/bin/bash

# Update rapidyenc submodule and copy to src directory

set -eo pipefail

cd $(dirname ${BASH_SOURCE[0]})

git submodule update --recursive --remote
rsync -a --exclude=.git rapidyenc/ src --delete

git add src