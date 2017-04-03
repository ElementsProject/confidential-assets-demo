#!/bin/bash
shopt -s expand_aliases

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
for i in alice bob charlie dave fred; do
    ${ELCLI} -datadir=${DEMOD}/data/${i} stop 2>/dev/null
    pkill -SIGINT $i
done
pkill ${ELDAE}
sleep 2

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
daemon=1
listen=1
txindex=1
keypool=10
EOF
    let PORT=PORT+10
    alias ${i}-dae="${ELDAE} -datadir=${DEMOD}/data/$i"
    alias ${i}-tx="${ELTX}"
    alias ${i}="${ELCLI} -datadir=${DEMOD}/data/$i"
done

fred-dae

LDW=1
while [ "${LDW}" = "1" ]
do
  LDW=0
  fred getwalletinfo > /dev/null 2>&1 || LDW=1
  if [ "${LDW}" = "1" ]; then
    sleep 1
  fi
done

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
    ${ELDAE} -datadir=${DEMOD}/data/$i
done

LDW=1
while [ "${LDW}" = "1" ]
do
  LDW=0
  alice getwalletinfo > /dev/null 2>&1 || LDW=1
  bob getwalletinfo > /dev/null 2>&1 || LDW=1
  charlie getwalletinfo > /dev/null 2>&1 || LDW=1
  dave getwalletinfo > /dev/null 2>&1 || LDW=1
  fred getwalletinfo > /dev/null 2>&1 || LDW=1
  if [ "${LDW}" = "1" ]; then
    echo -n -e "."
    sleep 1
  fi
done

echo " nodes started"

alice addnode 127.0.0.1:10011 onetry
bob addnode 127.0.0.1:10021 onetry
charlie addnode 127.0.0.1:10031 onetry
dave addnode 127.0.0.1:10041 onetry
fred addnode 127.0.0.1:10041 onetry

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

cd ${DEMOD}
for i in alice bob charlie dave fred; do
    ./$i &
done

cd "$(dirname "${BASH_SOURCE[0]}")"
sleep 2

echo "Setup complete. Use these URLs to test it out:"
echo "Alice -> http://127.0.0.1:8000/"
echo "Dave  -> http://127.0.0.1:8030/order.html"
echo "Dave  -> http://127.0.0.1:8030/list.html"
echo "When finished, run stop_demo.sh"
