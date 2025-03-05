#!/bin/bash
(cd ~ && nohup ~/app/pushchaind start --home ~/.push >> ~/app/chain.log 2>&1 &)