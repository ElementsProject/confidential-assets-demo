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
    ${ELCLI} -datadir=${DEMOD}/data/${i} stop
    pkill -SIGINT $i
    sleep 3
done
pkill ${ELDAE}
sleep 2

## cleanup previous data
rm -rf ${DEMOD}/data

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
    PORT=$(($PORT + 10))
    alias ${i}-dae="${ELDAE} -datadir=${DEMOD}/data/$i"
    alias ${i}-tx="${ELTX}"
    alias ${i}="${ELCLI} -datadir=${DEMOD}/data/$i"
    ${ELDAE} -datadir=${DEMOD}/data/$i
done

LDW=1
while [ "${LDW}" = "1" ]
do
  LDW=0
  alice getinfo > /dev/null 2>&1 || LDW=1
  bob getinfo > /dev/null 2>&1 || LDW=1
  charlie getinfo > /dev/null 2>&1 || LDW=1
  dave getinfo > /dev/null 2>&1 || LDW=1
  fred getinfo > /dev/null 2>&1 || LDW=1
  if [ "${LDW}" = "1" ]; then
    sleep 2
  fi
done

echo start

alice addnode 127.0.0.1:10011 onetry
bob addnode 127.0.0.1:10021 onetry
charlie addnode 127.0.0.1:10031 onetry
dave addnode 127.0.0.1:10041 onetry
fred addnode 127.0.0.1:10041 onetry
sleep 2

## generate point
fred generate 101
fred generateasset "AIRSKY"   1000000
fred generateasset "MELON" 2000000
fred generateasset "MONECRE" 2000000
fred getwalletinfo "*"

AIRSKY=$(fred dumpassetlabels | jq -r ".AIRSKY")
MELON=$(fred dumpassetlabels | jq -r ".MELON")
MONECRE=$(fred dumpassetlabels | jq -r ".MONECRE")

alice addassetlabel $AIRSKY "AIRSKY"
alice addassetlabel $MELON "MELON"
alice addassetlabel $MONECRE "MONECRE"
bob addassetlabel $AIRSKY "AIRSKY"
bob addassetlabel $MELON "MELON"
bob addassetlabel $MONECRE "MONECRE"
charlie addassetlabel $AIRSKY "AIRSKY"
charlie addassetlabel $MELON "MELON"
charlie addassetlabel $MONECRE "MONECRE"
dave addassetlabel $AIRSKY "AIRSKY"
dave addassetlabel $MELON "MELON"
dave addassetlabel $MONECRE "MONECRE"

## preset asset
echo AIRSKY
fred sendtoaddress $(alice validateaddress $(alice getnewaddress) | jq -r ".unconfidential") 500 "" "" false "AIRSKY"
sleep 1
echo MELON
fred sendtoaddress $(alice validateaddress $(alice getnewaddress) | jq -r ".unconfidential") 100 "" "" false "MELON"
sleep 1
echo MONECRE
fred sendtoaddress $(alice validateaddress $(alice getnewaddress) | jq -r ".unconfidential") 150 "" "" false "MONECRE"
sleep 1
fred generate 1
sleep 1 # wait for sync
alice getwalletinfo "*"

for i in 100 200 300 400 500; do
for j in AIRSKY MELON MONECRE; do
fred sendtoaddress $(charlie validateaddress $(charlie getnewaddress) | jq -r ".unconfidential") $i "" "" false "$j"
done
done
fred generate 1
sleep 1 # wait for sync
charlie getwalletinfo "*"

cd ${DEMOD}
for i in alice bob charlie dave fred; do
    ./$i &
done

cd "$(dirname "${BASH_SOURCE[0]}")"
sleep 2

echo "OK. Enjoy Demo."
echo "Alice -> http://127.0.0.1:8000/"
echo "Dave  -> http://127.0.0.1:8030/order.html"
echo "Dave  -> http://127.0.0.1:8030/list.html"
