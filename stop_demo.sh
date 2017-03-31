#!/bin/bash

# Stop the demo. This script is definitely not the way to do this in a production environment.
# Note that any running elementsd processes WILL be killed unless owned by a different user!

pkill bob
pkill dave
pkill charlie
pkill alice
pkill fred
pkill -9 elementsd
