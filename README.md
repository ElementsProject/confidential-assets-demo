# Confidential Asset Demo

## Introduction

The Confidential Transactions / Assets feature of Elements Core allows competing parties to interact
within the same block chain without revealing crucial information to each other, such as how much
of a given asset was transferred in a given period.

This is a simple demonstration showing a scenario where a coffee shop (Dave the merchant) charges
a customer (Alice) for coffee using a given type of asset, which the customer does not presently hold.
To facilitate the purchase, the customer makes use of a point exchange system (Charlie) to convert one
of their other assets into the one Dave accepts.

Bob is a competitor trying to gather info about Dave's sales. Due to the CT/CA feature of Elements
Core, the idea is that he will not see anything useful at all by processing transactions on the
block chain.

Fred is completely uninteresting but necessary as he makes blocks on the block chain when transactions
enter his mempool.

## Installing and set up

The demo is written in Go with some HTML/JS components for UI related stuff.

There are five nodes, one for each party mentioned above, as well as several assets that must be
generated and given to the appropriate party before the demo will function. This can be automated using
the setup-demo.sh script in the democode folder. This essentially does the following:

1. Sets up 5 Elements Core nodes and connects them to each other.
2. Generates the appropriate assets.
3. Sends assets to the appropriate parties.
4. Starts up the appropriate demo-specific daemons.

After this, open two pages in a web browser:
- http://localhost:8000 (the customer Alice's UI)
- http://localhost:8030 (the merchant Dave's UI)

The idea is that Dave presents Alice with his UI, and Alice uses her UI (some app) to perform the
exchange.
