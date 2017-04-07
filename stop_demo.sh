#!/bin/bash


if [ -e ./demo.tmp ]; then

    source ./demo.tmp

    gopids=( $dave_pid $alice_pid $charlie_pid $fred_pid $bob_pid )
    for pid in "${gopids[@]}"; do
        if ps -p $pid > /dev/null ; then
            echo "kill -SIGINT $pid"
            kill -SIGINT $pid
        fi
    done
    sleep 3
    for pid in "${gopids[@]}"; do
        if ps -p $pid > /dev/null ; then
            echo "kill -9 $pid"
            kill -9 $pid
        fi
    done

    dirs=( $dave_dir $alice_dir $charlie_dir $fred_dir $bob_dir )
    for dir in "${dirs[@]}"; do
        echo "$ELCLI $dir stop"
        $ELCLI $dir stop
    done
    sleep 3
    pids=( $dave_dae $alice_dae $charlie_dae $fred_dae $bob_dae )
    for pid in "${pids[@]}"; do
        if ps -p $pid > /dev/null ; then
            echo "kill -9 $pid"
            kill -9 $pid
        fi
    done

    rm -f ./demo.tmp

else

    echo "kill processes"

    # Stop the demo. This script is definitely not the way to do this in a production environment.
    # Note that any running elementsd processes WILL be killed unless owned by a different user!

    pkill bob
    pkill dave
    pkill charlie
    pkill alice
    pkill fred
    pkill -9 elementsd

fi
