#!/bin/bash
shopt -s expand_aliases

start_daemon () {
  if [ $# -le 1 ]; then
    echo " no nodes started."
    return 1
  fi

  for i in ${@:2:$#-1} ; do
    ${ELDAE} -datadir=${DEMOD}/data/${i} &
    if [ ""${1} == "1" ]; then
      echo "${i}_dae=$!" >> ./demo.tmp
    fi
  done

  LDW=1
  while [ "${LDW}" = "1" ]
  do
    LDW=0
    for i in ${@:2:$#-1} ; do
      ${ELCLI} -datadir=${DEMOD}/data/${i} getwalletinfo > /dev/null 2>&1 || LDW=1
    done
    if [ "${LDW}" = "1" ]; then
      echo -n -e "."
      sleep 1
    fi
  done
  echo " nodes started."
  return 0
}

# prepare
## be sure at elements-next folder
cd "$(dirname "${BASH_SOURCE[0]}")"
for i in elementsd elements-cli elements-tx ; do
	which $i > /dev/null
	if [ ""$? != "0" ];then
		echo "cannot find [" $i "]"
		exit 1
	fi
done

DEMOD=$PWD/demo
ELDAE=elementsd
ELCLI=elements-cli
ELTX=elements-tx

## cleanup previous data
if [ -e ./demo.tmp ]; then
    ./stop_demo.sh
fi
echo "ELCLI=$ELCLI" >> ./demo.tmp

## cleanup previous data
rm -rf ${DEMOD}/data

echo "initial setup - asset generation"

## setup nodes
PORT=0
for i in alice bob charlie dave fred; do
    mkdir -p ${DEMOD}/data/$i
    cat <<EOF > ${DEMOD}/data/$i/elements.conf
rpcuser=user
rpcpassword=pass
rpcport=$((10000 + $PORT))
port=$((10001 + $PORT))

connect=localhost:$((10001 + $(($PORT + 10)) % 50))
regtest=1
daemon=0
listen=1
txindex=1
keypool=10
EOF
    let PORT=PORT+10
    alias ${i}-dae="${ELDAE} -datadir=${DEMOD}/data/$i"
    alias ${i}-tx="${ELTX}"
    alias ${i}="${ELCLI} -datadir=${DEMOD}/data/$i"
    echo "${i}_dir=\"-datadir=${DEMOD}/data/$i\"" >> ./demo.tmp
done

start_daemon 0 fred

echo "- generating initial blocks to reach maturity"

## generate assets
fred generate 100 >/dev/null

echo -n -e "- generating AIRSKY asset"
AIRSKY=$(fred issueasset 1000000 500 | jq -r ".asset")
echo -n -e ": $AIRSKY\n- generating MELON asset"
MELON=$(fred issueasset 2000000 500 | jq -r ".asset")
echo -n -e ": $MELON\n- generating MONECRE asset"
MONECRE=$(fred issueasset 2000000 500 | jq -r ".asset")
echo ": $MONECRE"

echo -n -e "final setup - starting daemons"

fred stop
sleep 1

## setup nodes phase 2
for i in alice bob charlie dave fred; do
    cat <<EOF >> ${DEMOD}/data/$i/elements.conf
assetdir=$AIRSKY:AIRSKY
assetdir=$MELON:MELON
assetdir=$MONECRE:MONECRE
EOF
done

start_daemon 0 alice bob charlie dave fred

## generate assets
fred getwalletinfo

## preset asset
echo -n -e "AIRSKY"
# fred sendtoaddress $(alice validateaddress $(alice getnewaddress) | jq -r ".unconfidential") 500 "" "" false "AIRSKY" >/dev/null
fred sendtoaddress $(alice getnewaddress) 500 "" "" false "AIRSKY" >/dev/null
sleep 1
echo -n -e "\nMELON"
# fred sendtoaddress $(alice validateaddress $(alice getnewaddress) | jq -r ".unconfidential") 100 "" "" false "MELON" >/dev/null
fred sendtoaddress $(alice getnewaddress) 100 "" "" false "MELON" >/dev/null
sleep 1
echo -n -e "\nMONECRE"
# fred sendtoaddress $(alice validateaddress $(alice getnewaddress) | jq -r ".unconfidential") 150 "" "" false "MONECRE" >/dev/null
fred sendtoaddress $(alice getnewaddress) 150 "" "" false "MONECRE" >/dev/null
echo -n -e "\n"
fred generate 1 >/dev/null
sleep 1 # wait for sync
echo "Alice wallet:"
alice getwalletinfo

echo -n -e "Sending to Charlie [               ]\b\b\b\b\b\b\b\b\b\b\b\b\b\b\b\b"
for i in 100 200 300 400 500; do
  for j in AIRSKY MELON MONECRE; do
    # fred sendtoaddress $(charlie validateaddress $(charlie getnewaddress) | jq -r ".unconfidential") $i "" "" false "$j" >/dev/null
    fred sendtoaddress $(charlie getnewaddress) $i "" "" false "$j" >/dev/null
    echo -n -e "."
  done
done
echo ""
fred generate 1
sleep 1 # wait for sync
echo "Charlie wallet:"
charlie getwalletinfo

## setup nodes phase 3
alice stop
bob stop
charlie stop
dave stop
fred stop
sleep 3

for i in alice bob charlie dave fred; do
    cat <<EOF >> ${DEMOD}/data/$i/elements.conf
    feeasset=$AIRSKY
EOF
done

start_daemon 1 alice bob charlie dave fred

cd ${DEMOD}
for i in alice bob charlie dave fred; do
    ./$i &
    echo "${i}_pid=$!" >> ../demo.tmp
done

cd "$(dirname "${BASH_SOURCE[0]}")"
sleep 2

echo "Setup complete. Use these URLs to test it out:"
echo "Alice -> http://127.0.0.1:8000/"
echo "Dave  -> http://127.0.0.1:8030/order.html"
echo "Dave  -> http://127.0.0.1:8030/list.html"
echo "When finished, run stop_demo.sh"
