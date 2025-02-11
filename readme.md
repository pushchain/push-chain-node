### **System Requirements**

- **OS:** macOS, Linux, or WSL on Windows  
- **Go:** `>=1.21` (Check with `go version`)  
- **Node.js**  

## Steps:

1. [Install Ignite CLI](https://docs.ignite.com/welcome/install)  
2. Clone the repo:  

    ```sh
    git clone https://github.com/push-protocol/push-cosmos-devnet
    ```
3. Install the dependencies:  

    ```sh
    make install
    ```
4. To start the chain:  

    ```sh
    ignite chain serve
    ```

#### After this command runs successfully, you shall notice two addresses given, alice and bob, these can be used for all the testing and simulations. Also note that Alice is the default Validator, and Bob is the faucet, configured in the config.yml. 

# Common CLI Commands

### **1. Bank**
#### **Query balance:**  

    
    pushchaind query bank balances [address]
    

#### **Send tokens:**  

    
    pushchaind tx bank send [from-address] [to-address] [amount]
    

### **2. Tx Hash Info**  

    
    pushchaind query tx [tx-hash]
   

### **3. Fee Grant**
#### **For a transaction to use granter allowance, add this flag in the tx command:**  

    
    --fee-granter=[granter-address]
    

#### **Grant allowance:**  

    
    pushchaind tx feegrant grant [granter] [grantee]
    

##### **Flags:**
    
    --spend-limit         # The maximum amount of tokens the grantee can spend
    --period              # The time duration in seconds for periodic allowance
    --period-limit        # The maximum amount of tokens the grantee can spend within each period
    --expiration          # The date and time when the grant expires (RFC3339 format)
    --allowed-messages    # Comma-separated list of allowed message type URLs
    

#### **Query Fee Grants**
##### **All grants of a granter:**
   
    pushchaind query feegrant grants-by-granter [granter-address]
    
##### **All grants of a grantee:**
    
    pushchaind query feegrant grants-by-grantee [grantee-address]
    
##### **Specific grant:**
   
    pushchaind query feegrant grant [granter-addr] [grantee-addr]
   

### **4. Gas Prices**
##### **Add this flag to the transaction:**
    --gas-prices="[amount]"
  

### **5. Gas Limit**
##### **Add this flag to the transaction:**
    --gas [amount]
   
