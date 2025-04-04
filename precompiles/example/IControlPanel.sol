// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

/// @dev The IControlPanels contract's address.
address constant ICONTROLPANEL_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000000901;

/// @dev The IControlPanel contract's instance.
IControlPanel constant IControlPanel_CONTRACT = IControlPanel(ICONTROLPANEL_PRECOMPILE_ADDRESS);

struct ValidatorWhitelist{
    bool enabled;
    string[] addresses;
}

interface IControlPanel {
    // Txs
    function updateParams(string memory authority, string memory admin, bool enabled, string[] memory validators) external returns (bool success);

    // Queries
    event UpdateParams(string authority, string admin, bool enabled, string[] validators);
    function getParams() external view returns (address adminAddress, ValidatorWhitelist memory validator_whitelist);
}
