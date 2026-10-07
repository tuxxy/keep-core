// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package frostabi

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// FrostDkgValidatorResult is an auto generated low-level Go binding around an user-defined struct.
type FrostDkgValidatorResult struct {
	SubmitterMemberIndex     *big.Int
	GroupPubKey              []byte
	MisbehavedMembersIndices []uint8
	Signatures               []byte
	SigningMembersIndices    []*big.Int
	Members                  []uint32
	MembersHash              [32]byte
}

// FrostTypesDescriptor is an auto generated low-level Go binding around an user-defined struct.
type FrostTypesDescriptor struct {
	Scheme             uint8
	Profile            uint8
	ChainId            *big.Int
	Registry           common.Address
	Epoch              uint64
	Members            []uint32
	Operators          []common.Address
	Threshold          uint16
	SnowfallDescriptor [32]byte
	OutputKey          [32]byte
}

// FrostWalletRegistryEpochSnapshot is an auto generated low-level Go binding around an user-defined struct.
type FrostWalletRegistryEpochSnapshot struct {
	BlockNumber    uint64
	State          uint8
	Epoch          uint64
	SubmittedAt    uint64
	ResultDeadline uint64
	SubmittedHash  [32]byte
	Submitted      []byte
	ApprovedId     [32]byte
	Approved       FrostWalletRegistryWallet
}

// FrostWalletRegistryWallet is an auto generated low-level Go binding around an user-defined struct.
type FrostWalletRegistryWallet struct {
	Descriptor     FrostTypesDescriptor
	ResultHash     [32]byte
	DescriptorHash [32]byte
	ApprovalBlock  uint64
	Deadline       uint64
	State          uint8
}

// FrostDkgValidatorMetaData contains all meta data concerning the FrostDkgValidator contract.
var FrostDkgValidatorMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"n\",\"type\":\"uint16\"},{\"internalType\":\"uint16\",\"name\":\"t\",\"type\":\"uint16\"},{\"internalType\":\"uint16\",\"name\":\"r\",\"type\":\"uint16\"},{\"internalType\":\"uint16\",\"name\":\"b\",\"type\":\"uint16\"},{\"internalType\":\"uint16\",\"name\":\"f\",\"type\":\"uint16\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"PROFILE\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"READY_DOMAIN\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"RESULT_DOMAIN\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"falseReadySeats\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"groupSize\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptor\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"uint32\",\"name\":\"generation\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"capabilityVersion\",\"type\":\"uint32\"},{\"internalType\":\"uint16\",\"name\":\"seat\",\"type\":\"uint16\"},{\"internalType\":\"bytes32\",\"name\":\"referenceHash\",\"type\":\"bytes32\"}],\"name\":\"readinessDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"readySeats\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"submitterMemberIndex\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"groupPubKey\",\"type\":\"bytes\"},{\"internalType\":\"uint8[]\",\"name\":\"misbehavedMembersIndices\",\"type\":\"uint8[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"uint256[]\",\"name\":\"signingMembersIndices\",\"type\":\"uint256[]\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"bytes32\",\"name\":\"membersHash\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDkgValidator.Result\",\"name\":\"result\",\"type\":\"tuple\"}],\"name\":\"resultDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"threshold\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"unavailableSeats\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"address[]\",\"name\":\"operators\",\"type\":\"address[]\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"submitterMemberIndex\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"groupPubKey\",\"type\":\"bytes\"},{\"internalType\":\"uint8[]\",\"name\":\"misbehavedMembersIndices\",\"type\":\"uint8[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"uint256[]\",\"name\":\"signingMembersIndices\",\"type\":\"uint256[]\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"bytes32\",\"name\":\"membersHash\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDkgValidator.Result\",\"name\":\"result\",\"type\":\"tuple\"}],\"name\":\"validate\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x6101206040523480156200001257600080fd5b506040516200123038038062001230833981016040819052620000359162000199565b60008561ffff161180156200004f575060648561ffff1611155b80156200006f57506200006460028662000209565b61ffff168461ffff16115b80156200008457508461ffff168461ffff1611155b620000cb5760405162461bcd60e51b815260206004820152601260248201527146524f53542067726f757020706f6c69637960701b60448201526064015b60405180910390fd5b8461ffff168361ffff16111580156200010e57508061ffff168261ffff168561ffff16620000fa919062000239565b62000106919062000239565b8361ffff1610155b6200015c5760405162461bcd60e51b815260206004820152601660248201527f46524f53542072656164696e65737320706f6c696379000000000000000000006044820152606401620000c2565b61ffff94851660805292841660a05290831660c052821660e052166101005262000261565b805161ffff811681146200019457600080fd5b919050565b600080600080600060a08688031215620001b257600080fd5b620001bd8662000181565b9450620001cd6020870162000181565b9350620001dd6040870162000181565b9250620001ed6060870162000181565b9150620001fd6080870162000181565b90509295509295909350565b600061ffff808416806200022d57634e487b7160e01b600052601260045260246000fd5b92169190910492915050565b808201808211156200025b57634e487b7160e01b600052601160045260246000fd5b92915050565b60805160a05160c05160e05161010051610f7d620002b360003960006101c50152600060ed01526000610226015260008181610154015261077a01526000818161019e015261030a0152610f7d6000f3fe608060405234801561001057600080fd5b50600436106100a95760003560e01c80635694bcce116100715780635694bcce1461017657806363b635ea14610199578063726b5f9a146101c057806378efb5c4146101e757806385cc6fed1461020e578063bfac640e1461022157600080fd5b806316524865146100ae57806332015ad5146100e857806333329811146101225780633d0a65e51461013c57806342cde4e81461014f575b600080fd5b6100d57fe55aac39b908b77d7b895e7cb21d749909267f67412caab530aa1ea0635c431181565b6040519081526020015b60405180910390f35b61010f7f000000000000000000000000000000000000000000000000000000000000000081565b60405161ffff90911681526020016100df565b61012a600181565b60405160ff90911681526020016100df565b6100d561014a366004610a0e565b610248565b61010f7f000000000000000000000000000000000000000000000000000000000000000081565b610189610184366004610af8565b610303565b60405190151581526020016100df565b61010f7f000000000000000000000000000000000000000000000000000000000000000081565b61010f7f000000000000000000000000000000000000000000000000000000000000000081565b6100d57fb8b1cfc222a21af48b4cff3c28ae5175c29abe929c6e02097c6fb7712f17710081565b6100d561021c366004610bbd565b61072a565b61010f7f000000000000000000000000000000000000000000000000000000000000000081565b604080517fe55aac39b908b77d7b895e7cb21d749909267f67412caab530aa1ea0635c4311602082015246918101919091526001600160a01b03871660608201526001608082015260a0810189905260c0810188905267ffffffffffffffff861660e082015263ffffffff808616610100830152841661012082015261ffff83166101408201526101608101829052600090610180016040516020818303038152906040528051906020012090505b98975050505050505050565b600061ffff7f00000000000000000000000000000000000000000000000000000000000000001685811415806103395750838114155b8061035257508061034d60a0850185610c2c565b905014155b8061036b5750806103666080850185610c2c565b905014155b8061038e575061037c816041610c8c565b6103896060850185610ca9565b905014155b806103a857506103a16020840184610ca9565b9050604114155b806103e1575060016103bd6020850185610ca9565b60008181106103ce576103ce610cf0565b9050013560f81c60f81b60f81c60ff1614155b806103f957506103f46040840184610c2c565b151590505b8061040357508235155b8061040e5750823581105b1561041d5760009150506102f7565b8686604051602001610430929190610d4a565b604051602081830303815290604052805190602001208360c0013514158061048f575060c083013561046560a0850185610c2c565b604051602001610476929190610d4a565b6040516020818303038152906040528051906020012014155b1561049e5760009150506102f7565b60006104ad6020850185610ca9565b6104bc91602191600191610d66565b6104c591610d90565b90506104d0816107d4565b1580610500575060006104e66020860186610ca9565b6104f591604191602191610d66565b6104fe91610d90565b145b15610510576000925050506102f7565b60006105546105218d8d8d8961072a565b7f19457468657265756d205369676e6564204d6573736167653a0a3332000000006000908152601c91909152603c902090565b905060005b838110156107175789898281811061057357610573610cf0565b90506020020160208101906105889190610dae565b63ffffffff1615806105c9575060008888838181106105a9576105a9610cf0565b90506020020160208101906105be9190610dd0565b6001600160a01b0316145b8061060157506105da816001610deb565b6105e76080880188610c2c565b838181106105f7576105f7610cf0565b9050602002013514155b156106135760009450505050506102f7565b6000806106908461062760608b018b610ca9565b610632876041610c8c565b9061063e886001610deb565b610649906041610c8c565b9261065693929190610d66565b8080601f01602080910402602001604051908101604052809392919081815260200183838082843760009201919091525061089f92505050565b909250905060008160048111156106a9576106a9610dfe565b1415806106ee57508989848181106106c3576106c3610cf0565b90506020020160208101906106d89190610dd0565b6001600160a01b0316826001600160a01b031614155b1561070257600096505050505050506102f7565b5050808061070f90610e14565b915050610559565b5060019c9b505050505050505050505050565b60007fb8b1cfc222a21af48b4cff3c28ae5175c29abe929c6e02097c6fb7712f1771004686868661075e6020880188610ca9565b61076b60408a018a610c2c565b61077860a08c018c610c2c565b7f00000000000000000000000000000000000000000000000000000000000000006040516020016107b49c9b9a99989796959493929190610e76565b604051602081830303815290604052805190602001209050949350505050565b6000816401000003d01981106107ed5750600092915050565b60006401000003d01960076401000003d019846401000003d0198687090908905060006040518060c0016040528060208152602001602081526020016020815260200183815260200160046401000003d019600161084b9190610deb565b6108559190610f25565b81526020016401000003d019815250905061086e6109a8565b600060208260c08560055afa90508080156108945750815184906401000003d019908009145b979650505050505050565b60008082516041036108d55760208301516040840151606085015160001a6108c9878285856108e4565b945094505050506108dd565b506000905060025b9250929050565b6000807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a083111561091b575060009050600361099f565b6040805160008082526020820180845289905260ff881692820192909252606081018690526080810185905260019060a0016020604051602081039080840390855afa15801561096f573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b0381166109985760006001925092505061099f565b9150600090505b94509492505050565b60405180602001604052806001906020820280368337509192915050565b80356001600160a01b03811681146109dd57600080fd5b919050565b803567ffffffffffffffff811681146109dd57600080fd5b803563ffffffff811681146109dd57600080fd5b600080600080600080600080610100898b031215610a2b57600080fd5b8835975060208901359650610a4260408a016109c6565b9550610a5060608a016109e2565b9450610a5e60808a016109fa565b9350610a6c60a08a016109fa565b925060c089013561ffff81168114610a8357600080fd5b8092505060e089013590509295985092959890939650565b60008083601f840112610aad57600080fd5b50813567ffffffffffffffff811115610ac557600080fd5b6020830191508360208260051b85010111156108dd57600080fd5b600060e08284031215610af257600080fd5b50919050565b60008060008060008060008060c0898b031215610b1457600080fd5b610b1d896109c6565b9750610b2b60208a016109c6565b9650610b3960408a016109e2565b9550606089013567ffffffffffffffff80821115610b5657600080fd5b610b628c838d01610a9b565b909750955060808b0135915080821115610b7b57600080fd5b610b878c838d01610a9b565b909550935060a08b0135915080821115610ba057600080fd5b50610bad8b828c01610ae0565b9150509295985092959890939650565b60008060008060808587031215610bd357600080fd5b610bdc856109c6565b9350610bea602086016109c6565b9250610bf8604086016109e2565b9150606085013567ffffffffffffffff811115610c1457600080fd5b610c2087828801610ae0565b91505092959194509250565b6000808335601e19843603018112610c4357600080fd5b83018035915067ffffffffffffffff821115610c5e57600080fd5b6020019150600581901b36038213156108dd57600080fd5b634e487b7160e01b600052601160045260246000fd5b8082028115828204841417610ca357610ca3610c76565b92915050565b6000808335601e19843603018112610cc057600080fd5b83018035915067ffffffffffffffff821115610cdb57600080fd5b6020019150368190038213156108dd57600080fd5b634e487b7160e01b600052603260045260246000fd5b8183526000602080850194508260005b85811015610d3f5763ffffffff610d2c836109fa565b1687529582019590820190600101610d16565b509495945050505050565b602081526000610d5e602083018486610d06565b949350505050565b60008085851115610d7657600080fd5b83861115610d8357600080fd5b5050820193919092039150565b80356020831015610ca357600019602084900360031b1b1692915050565b600060208284031215610dc057600080fd5b610dc9826109fa565b9392505050565b600060208284031215610de257600080fd5b610dc9826109c6565b80820180821115610ca357610ca3610c76565b634e487b7160e01b600052602160045260246000fd5b600060018201610e2657610e26610c76565b5060010190565b818352600060208085019450826000805b86811015610e6a57823560ff8116808214610e57578384fd5b8952509683019691830191600101610e3e565b50959695505050505050565b8c8152602081018c90526001600160a01b038b811660408301528a16606082015267ffffffffffffffff8916608082015261012060a0820181905281018790526000610140888a828501376000838a01820152601f8901601f19168301838103820160c0850152610eea818301898b610e2d565b91505082810360e0840152610f00818688610d06565b915050610f1461010083018461ffff169052565b9d9c50505050505050505050505050565b600082610f4257634e487b7160e01b600052601260045260246000fd5b50049056fea2646970667358221220498da50369884f4a7e45c275a47663b3eee61757b985e21198600f3f715c330b64736f6c63430008110033",
}

// FrostDkgValidatorABI is the input ABI used to generate the binding from.
// Deprecated: Use FrostDkgValidatorMetaData.ABI instead.
var FrostDkgValidatorABI = FrostDkgValidatorMetaData.ABI

// FrostDkgValidatorBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use FrostDkgValidatorMetaData.Bin instead.
var FrostDkgValidatorBin = FrostDkgValidatorMetaData.Bin

// DeployFrostDkgValidator deploys a new Ethereum contract, binding an instance of FrostDkgValidator to it.
func DeployFrostDkgValidator(auth *bind.TransactOpts, backend bind.ContractBackend, n uint16, t uint16, r uint16, b uint16, f uint16) (common.Address, *types.Transaction, *FrostDkgValidator, error) {
	parsed, err := FrostDkgValidatorMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(FrostDkgValidatorBin), backend, n, t, r, b, f)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &FrostDkgValidator{FrostDkgValidatorCaller: FrostDkgValidatorCaller{contract: contract}, FrostDkgValidatorTransactor: FrostDkgValidatorTransactor{contract: contract}, FrostDkgValidatorFilterer: FrostDkgValidatorFilterer{contract: contract}}, nil
}

// FrostDkgValidator is an auto generated Go binding around an Ethereum contract.
type FrostDkgValidator struct {
	FrostDkgValidatorCaller     // Read-only binding to the contract
	FrostDkgValidatorTransactor // Write-only binding to the contract
	FrostDkgValidatorFilterer   // Log filterer for contract events
}

// FrostDkgValidatorCaller is an auto generated read-only Go binding around an Ethereum contract.
type FrostDkgValidatorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostDkgValidatorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FrostDkgValidatorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostDkgValidatorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FrostDkgValidatorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostDkgValidatorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FrostDkgValidatorSession struct {
	Contract     *FrostDkgValidator // Generic contract binding to set the session for
	CallOpts     bind.CallOpts      // Call options to use throughout this session
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// FrostDkgValidatorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FrostDkgValidatorCallerSession struct {
	Contract *FrostDkgValidatorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts            // Call options to use throughout this session
}

// FrostDkgValidatorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FrostDkgValidatorTransactorSession struct {
	Contract     *FrostDkgValidatorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// FrostDkgValidatorRaw is an auto generated low-level Go binding around an Ethereum contract.
type FrostDkgValidatorRaw struct {
	Contract *FrostDkgValidator // Generic contract binding to access the raw methods on
}

// FrostDkgValidatorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FrostDkgValidatorCallerRaw struct {
	Contract *FrostDkgValidatorCaller // Generic read-only contract binding to access the raw methods on
}

// FrostDkgValidatorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FrostDkgValidatorTransactorRaw struct {
	Contract *FrostDkgValidatorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFrostDkgValidator creates a new instance of FrostDkgValidator, bound to a specific deployed contract.
func NewFrostDkgValidator(address common.Address, backend bind.ContractBackend) (*FrostDkgValidator, error) {
	contract, err := bindFrostDkgValidator(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FrostDkgValidator{FrostDkgValidatorCaller: FrostDkgValidatorCaller{contract: contract}, FrostDkgValidatorTransactor: FrostDkgValidatorTransactor{contract: contract}, FrostDkgValidatorFilterer: FrostDkgValidatorFilterer{contract: contract}}, nil
}

// NewFrostDkgValidatorCaller creates a new read-only instance of FrostDkgValidator, bound to a specific deployed contract.
func NewFrostDkgValidatorCaller(address common.Address, caller bind.ContractCaller) (*FrostDkgValidatorCaller, error) {
	contract, err := bindFrostDkgValidator(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FrostDkgValidatorCaller{contract: contract}, nil
}

// NewFrostDkgValidatorTransactor creates a new write-only instance of FrostDkgValidator, bound to a specific deployed contract.
func NewFrostDkgValidatorTransactor(address common.Address, transactor bind.ContractTransactor) (*FrostDkgValidatorTransactor, error) {
	contract, err := bindFrostDkgValidator(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FrostDkgValidatorTransactor{contract: contract}, nil
}

// NewFrostDkgValidatorFilterer creates a new log filterer instance of FrostDkgValidator, bound to a specific deployed contract.
func NewFrostDkgValidatorFilterer(address common.Address, filterer bind.ContractFilterer) (*FrostDkgValidatorFilterer, error) {
	contract, err := bindFrostDkgValidator(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FrostDkgValidatorFilterer{contract: contract}, nil
}

// bindFrostDkgValidator binds a generic wrapper to an already deployed contract.
func bindFrostDkgValidator(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FrostDkgValidatorMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FrostDkgValidator *FrostDkgValidatorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FrostDkgValidator.Contract.FrostDkgValidatorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FrostDkgValidator *FrostDkgValidatorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostDkgValidator.Contract.FrostDkgValidatorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FrostDkgValidator *FrostDkgValidatorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FrostDkgValidator.Contract.FrostDkgValidatorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FrostDkgValidator *FrostDkgValidatorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FrostDkgValidator.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FrostDkgValidator *FrostDkgValidatorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostDkgValidator.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FrostDkgValidator *FrostDkgValidatorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FrostDkgValidator.Contract.contract.Transact(opts, method, params...)
}

// PROFILE is a free data retrieval call binding the contract method 0x33329811.
//
// Solidity: function PROFILE() view returns(uint8)
func (_FrostDkgValidator *FrostDkgValidatorCaller) PROFILE(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "PROFILE")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// PROFILE is a free data retrieval call binding the contract method 0x33329811.
//
// Solidity: function PROFILE() view returns(uint8)
func (_FrostDkgValidator *FrostDkgValidatorSession) PROFILE() (uint8, error) {
	return _FrostDkgValidator.Contract.PROFILE(&_FrostDkgValidator.CallOpts)
}

// PROFILE is a free data retrieval call binding the contract method 0x33329811.
//
// Solidity: function PROFILE() view returns(uint8)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) PROFILE() (uint8, error) {
	return _FrostDkgValidator.Contract.PROFILE(&_FrostDkgValidator.CallOpts)
}

// READYDOMAIN is a free data retrieval call binding the contract method 0x16524865.
//
// Solidity: function READY_DOMAIN() view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCaller) READYDOMAIN(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "READY_DOMAIN")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// READYDOMAIN is a free data retrieval call binding the contract method 0x16524865.
//
// Solidity: function READY_DOMAIN() view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorSession) READYDOMAIN() ([32]byte, error) {
	return _FrostDkgValidator.Contract.READYDOMAIN(&_FrostDkgValidator.CallOpts)
}

// READYDOMAIN is a free data retrieval call binding the contract method 0x16524865.
//
// Solidity: function READY_DOMAIN() view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) READYDOMAIN() ([32]byte, error) {
	return _FrostDkgValidator.Contract.READYDOMAIN(&_FrostDkgValidator.CallOpts)
}

// RESULTDOMAIN is a free data retrieval call binding the contract method 0x78efb5c4.
//
// Solidity: function RESULT_DOMAIN() view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCaller) RESULTDOMAIN(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "RESULT_DOMAIN")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// RESULTDOMAIN is a free data retrieval call binding the contract method 0x78efb5c4.
//
// Solidity: function RESULT_DOMAIN() view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorSession) RESULTDOMAIN() ([32]byte, error) {
	return _FrostDkgValidator.Contract.RESULTDOMAIN(&_FrostDkgValidator.CallOpts)
}

// RESULTDOMAIN is a free data retrieval call binding the contract method 0x78efb5c4.
//
// Solidity: function RESULT_DOMAIN() view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) RESULTDOMAIN() ([32]byte, error) {
	return _FrostDkgValidator.Contract.RESULTDOMAIN(&_FrostDkgValidator.CallOpts)
}

// FalseReadySeats is a free data retrieval call binding the contract method 0x32015ad5.
//
// Solidity: function falseReadySeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCaller) FalseReadySeats(opts *bind.CallOpts) (uint16, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "falseReadySeats")

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// FalseReadySeats is a free data retrieval call binding the contract method 0x32015ad5.
//
// Solidity: function falseReadySeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorSession) FalseReadySeats() (uint16, error) {
	return _FrostDkgValidator.Contract.FalseReadySeats(&_FrostDkgValidator.CallOpts)
}

// FalseReadySeats is a free data retrieval call binding the contract method 0x32015ad5.
//
// Solidity: function falseReadySeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) FalseReadySeats() (uint16, error) {
	return _FrostDkgValidator.Contract.FalseReadySeats(&_FrostDkgValidator.CallOpts)
}

// GroupSize is a free data retrieval call binding the contract method 0x63b635ea.
//
// Solidity: function groupSize() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCaller) GroupSize(opts *bind.CallOpts) (uint16, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "groupSize")

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// GroupSize is a free data retrieval call binding the contract method 0x63b635ea.
//
// Solidity: function groupSize() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorSession) GroupSize() (uint16, error) {
	return _FrostDkgValidator.Contract.GroupSize(&_FrostDkgValidator.CallOpts)
}

// GroupSize is a free data retrieval call binding the contract method 0x63b635ea.
//
// Solidity: function groupSize() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) GroupSize() (uint16, error) {
	return _FrostDkgValidator.Contract.GroupSize(&_FrostDkgValidator.CallOpts)
}

// ReadinessDigest is a free data retrieval call binding the contract method 0x3d0a65e5.
//
// Solidity: function readinessDigest(bytes32 walletId, bytes32 descriptor, address registry, uint64 epoch, uint32 generation, uint32 capabilityVersion, uint16 seat, bytes32 referenceHash) view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCaller) ReadinessDigest(opts *bind.CallOpts, walletId [32]byte, descriptor [32]byte, registry common.Address, epoch uint64, generation uint32, capabilityVersion uint32, seat uint16, referenceHash [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "readinessDigest", walletId, descriptor, registry, epoch, generation, capabilityVersion, seat, referenceHash)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ReadinessDigest is a free data retrieval call binding the contract method 0x3d0a65e5.
//
// Solidity: function readinessDigest(bytes32 walletId, bytes32 descriptor, address registry, uint64 epoch, uint32 generation, uint32 capabilityVersion, uint16 seat, bytes32 referenceHash) view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorSession) ReadinessDigest(walletId [32]byte, descriptor [32]byte, registry common.Address, epoch uint64, generation uint32, capabilityVersion uint32, seat uint16, referenceHash [32]byte) ([32]byte, error) {
	return _FrostDkgValidator.Contract.ReadinessDigest(&_FrostDkgValidator.CallOpts, walletId, descriptor, registry, epoch, generation, capabilityVersion, seat, referenceHash)
}

// ReadinessDigest is a free data retrieval call binding the contract method 0x3d0a65e5.
//
// Solidity: function readinessDigest(bytes32 walletId, bytes32 descriptor, address registry, uint64 epoch, uint32 generation, uint32 capabilityVersion, uint16 seat, bytes32 referenceHash) view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) ReadinessDigest(walletId [32]byte, descriptor [32]byte, registry common.Address, epoch uint64, generation uint32, capabilityVersion uint32, seat uint16, referenceHash [32]byte) ([32]byte, error) {
	return _FrostDkgValidator.Contract.ReadinessDigest(&_FrostDkgValidator.CallOpts, walletId, descriptor, registry, epoch, generation, capabilityVersion, seat, referenceHash)
}

// ReadySeats is a free data retrieval call binding the contract method 0xbfac640e.
//
// Solidity: function readySeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCaller) ReadySeats(opts *bind.CallOpts) (uint16, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "readySeats")

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// ReadySeats is a free data retrieval call binding the contract method 0xbfac640e.
//
// Solidity: function readySeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorSession) ReadySeats() (uint16, error) {
	return _FrostDkgValidator.Contract.ReadySeats(&_FrostDkgValidator.CallOpts)
}

// ReadySeats is a free data retrieval call binding the contract method 0xbfac640e.
//
// Solidity: function readySeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) ReadySeats() (uint16, error) {
	return _FrostDkgValidator.Contract.ReadySeats(&_FrostDkgValidator.CallOpts)
}

// ResultDigest is a free data retrieval call binding the contract method 0x85cc6fed.
//
// Solidity: function resultDigest(address registry, address pool, uint64 epoch, (uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCaller) ResultDigest(opts *bind.CallOpts, registry common.Address, pool common.Address, epoch uint64, result FrostDkgValidatorResult) ([32]byte, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "resultDigest", registry, pool, epoch, result)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ResultDigest is a free data retrieval call binding the contract method 0x85cc6fed.
//
// Solidity: function resultDigest(address registry, address pool, uint64 epoch, (uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorSession) ResultDigest(registry common.Address, pool common.Address, epoch uint64, result FrostDkgValidatorResult) ([32]byte, error) {
	return _FrostDkgValidator.Contract.ResultDigest(&_FrostDkgValidator.CallOpts, registry, pool, epoch, result)
}

// ResultDigest is a free data retrieval call binding the contract method 0x85cc6fed.
//
// Solidity: function resultDigest(address registry, address pool, uint64 epoch, (uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) view returns(bytes32)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) ResultDigest(registry common.Address, pool common.Address, epoch uint64, result FrostDkgValidatorResult) ([32]byte, error) {
	return _FrostDkgValidator.Contract.ResultDigest(&_FrostDkgValidator.CallOpts, registry, pool, epoch, result)
}

// Threshold is a free data retrieval call binding the contract method 0x42cde4e8.
//
// Solidity: function threshold() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCaller) Threshold(opts *bind.CallOpts) (uint16, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "threshold")

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// Threshold is a free data retrieval call binding the contract method 0x42cde4e8.
//
// Solidity: function threshold() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorSession) Threshold() (uint16, error) {
	return _FrostDkgValidator.Contract.Threshold(&_FrostDkgValidator.CallOpts)
}

// Threshold is a free data retrieval call binding the contract method 0x42cde4e8.
//
// Solidity: function threshold() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) Threshold() (uint16, error) {
	return _FrostDkgValidator.Contract.Threshold(&_FrostDkgValidator.CallOpts)
}

// UnavailableSeats is a free data retrieval call binding the contract method 0x726b5f9a.
//
// Solidity: function unavailableSeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCaller) UnavailableSeats(opts *bind.CallOpts) (uint16, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "unavailableSeats")

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// UnavailableSeats is a free data retrieval call binding the contract method 0x726b5f9a.
//
// Solidity: function unavailableSeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorSession) UnavailableSeats() (uint16, error) {
	return _FrostDkgValidator.Contract.UnavailableSeats(&_FrostDkgValidator.CallOpts)
}

// UnavailableSeats is a free data retrieval call binding the contract method 0x726b5f9a.
//
// Solidity: function unavailableSeats() view returns(uint16)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) UnavailableSeats() (uint16, error) {
	return _FrostDkgValidator.Contract.UnavailableSeats(&_FrostDkgValidator.CallOpts)
}

// Validate is a free data retrieval call binding the contract method 0x5694bcce.
//
// Solidity: function validate(address registry, address pool, uint64 epoch, uint32[] members, address[] operators, (uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) view returns(bool)
func (_FrostDkgValidator *FrostDkgValidatorCaller) Validate(opts *bind.CallOpts, registry common.Address, pool common.Address, epoch uint64, members []uint32, operators []common.Address, result FrostDkgValidatorResult) (bool, error) {
	var out []interface{}
	err := _FrostDkgValidator.contract.Call(opts, &out, "validate", registry, pool, epoch, members, operators, result)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Validate is a free data retrieval call binding the contract method 0x5694bcce.
//
// Solidity: function validate(address registry, address pool, uint64 epoch, uint32[] members, address[] operators, (uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) view returns(bool)
func (_FrostDkgValidator *FrostDkgValidatorSession) Validate(registry common.Address, pool common.Address, epoch uint64, members []uint32, operators []common.Address, result FrostDkgValidatorResult) (bool, error) {
	return _FrostDkgValidator.Contract.Validate(&_FrostDkgValidator.CallOpts, registry, pool, epoch, members, operators, result)
}

// Validate is a free data retrieval call binding the contract method 0x5694bcce.
//
// Solidity: function validate(address registry, address pool, uint64 epoch, uint32[] members, address[] operators, (uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) view returns(bool)
func (_FrostDkgValidator *FrostDkgValidatorCallerSession) Validate(registry common.Address, pool common.Address, epoch uint64, members []uint32, operators []common.Address, result FrostDkgValidatorResult) (bool, error) {
	return _FrostDkgValidator.Contract.Validate(&_FrostDkgValidator.CallOpts, registry, pool, epoch, members, operators, result)
}

// FrostWalletRegistryMetaData contains all meta data concerning the FrostWalletRegistry contract.
var FrostWalletRegistryMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"contractSortitionPool\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"contractFrostDkgValidator\",\"name\":\"policy\",\"type\":\"address\"},{\"internalType\":\"contractIFrostAdmission\",\"name\":\"eligibility\",\"type\":\"address\"},{\"internalType\":\"contractIRandomBeacon\",\"name\":\"beacon\",\"type\":\"address\"},{\"internalType\":\"contractIFrostWalletOwner\",\"name\":\"bridge\",\"type\":\"address\"},{\"internalType\":\"uint64[4]\",\"name\":\"periods\",\"type\":\"uint64[4]\"},{\"internalType\":\"uint16\",\"name\":\"poolLimit\",\"type\":\"uint16\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"}],\"name\":\"DkgExpired\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"indexed\":false,\"internalType\":\"address[]\",\"name\":\"operators\",\"type\":\"address[]\"}],\"name\":\"DkgStarted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"}],\"name\":\"ReadinessAccepted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"RequestsEnabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"}],\"name\":\"ResultApproved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"}],\"name\":\"ResultChallenged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"}],\"name\":\"ResultSubmitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"walletId\",\"type\":\"bytes32\"}],\"name\":\"WalletExpired\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"seed\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"__beaconCallback\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"admission\",\"outputs\":[{\"internalType\":\"contractIFrostAdmission\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"approveDkgResult\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"name\":\"approvedWallet\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"challengeDkgResult\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"challengePeriod\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"epoch\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"startBlock\",\"type\":\"uint64\"}],\"name\":\"epochView\",\"outputs\":[{\"components\":[{\"internalType\":\"uint64\",\"name\":\"blockNumber\",\"type\":\"uint64\"},{\"internalType\":\"enumFrostWalletRegistry.DkgState\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"submittedAt\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"resultDeadline\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"submittedHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"submitted\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"approvedId\",\"type\":\"bytes32\"},{\"components\":[{\"components\":[{\"internalType\":\"uint8\",\"name\":\"scheme\",\"type\":\"uint8\"},{\"internalType\":\"uint8\",\"name\":\"profile\",\"type\":\"uint8\"},{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"address[]\",\"name\":\"operators\",\"type\":\"address[]\"},{\"internalType\":\"uint16\",\"name\":\"threshold\",\"type\":\"uint16\"},{\"internalType\":\"bytes32\",\"name\":\"snowfallDescriptor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputKey\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostTypes.Descriptor\",\"name\":\"descriptor\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"approvalBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"deadline\",\"type\":\"uint64\"},{\"internalType\":\"enumFrostWalletRegistry.WalletState\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structFrostWalletRegistry.Wallet\",\"name\":\"approved\",\"type\":\"tuple\"}],\"internalType\":\"structFrostWalletRegistry.EpochSnapshot\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"expireDkg\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"}],\"name\":\"expireWallet\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"submitterMemberIndex\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"groupPubKey\",\"type\":\"bytes\"},{\"internalType\":\"uint8[]\",\"name\":\"misbehavedMembersIndices\",\"type\":\"uint8[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"uint256[]\",\"name\":\"signingMembersIndices\",\"type\":\"uint256[]\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"bytes32\",\"name\":\"membersHash\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDkgValidator.Result\",\"name\":\"result\",\"type\":\"tuple\"},{\"internalType\":\"uint64\",\"name\":\"startBlock\",\"type\":\"uint64\"}],\"name\":\"isResultValid\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"maxPoolOperators\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"outputKeyUsed\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"randomBeacon\",\"outputs\":[{\"internalType\":\"contractIRandomBeacon\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"readinessPeriod\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"operator\",\"type\":\"address\"}],\"name\":\"refreshOperator\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"requestNewWallet\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"requestsEnabled\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"resultDeadline\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"resultTimeout\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"seedTimeout\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"startBlock\",\"type\":\"uint64\"}],\"name\":\"selection\",\"outputs\":[{\"internalType\":\"uint32[]\",\"name\":\"\",\"type\":\"uint32[]\"},{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"setRequestsEnabled\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"sortitionPool\",\"outputs\":[{\"internalType\":\"contractSortitionPool\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"state\",\"outputs\":[{\"internalType\":\"enumFrostWalletRegistry.DkgState\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"submitterMemberIndex\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"groupPubKey\",\"type\":\"bytes\"},{\"internalType\":\"uint8[]\",\"name\":\"misbehavedMembersIndices\",\"type\":\"uint8[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"uint256[]\",\"name\":\"signingMembersIndices\",\"type\":\"uint256[]\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"bytes32\",\"name\":\"membersHash\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDkgValidator.Result\",\"name\":\"result\",\"type\":\"tuple\"}],\"name\":\"submitDkgResult\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"},{\"internalType\":\"uint16[]\",\"name\":\"seats\",\"type\":\"uint16[]\"},{\"internalType\":\"bytes32[]\",\"name\":\"references\",\"type\":\"bytes32[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"}],\"name\":\"submitReadinessV1\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"submittedAt\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"submittedHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"submittedResult\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"submitterMemberIndex\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"groupPubKey\",\"type\":\"bytes\"},{\"internalType\":\"uint8[]\",\"name\":\"misbehavedMembersIndices\",\"type\":\"uint8[]\"},{\"internalType\":\"bytes\",\"name\":\"signatures\",\"type\":\"bytes\"},{\"internalType\":\"uint256[]\",\"name\":\"signingMembersIndices\",\"type\":\"uint256[]\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"bytes32\",\"name\":\"membersHash\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostDkgValidator.Result\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"validator\",\"outputs\":[{\"internalType\":\"contractFrostDkgValidator\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"}],\"name\":\"wallet\",\"outputs\":[{\"components\":[{\"components\":[{\"internalType\":\"uint8\",\"name\":\"scheme\",\"type\":\"uint8\"},{\"internalType\":\"uint8\",\"name\":\"profile\",\"type\":\"uint8\"},{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"registry\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"uint32[]\",\"name\":\"members\",\"type\":\"uint32[]\"},{\"internalType\":\"address[]\",\"name\":\"operators\",\"type\":\"address[]\"},{\"internalType\":\"uint16\",\"name\":\"threshold\",\"type\":\"uint16\"},{\"internalType\":\"bytes32\",\"name\":\"snowfallDescriptor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputKey\",\"type\":\"bytes32\"}],\"internalType\":\"structFrostTypes.Descriptor\",\"name\":\"descriptor\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"descriptorHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"approvalBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"deadline\",\"type\":\"uint64\"},{\"internalType\":\"enumFrostWalletRegistry.WalletState\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structFrostWalletRegistry.Wallet\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"walletOwner\",\"outputs\":[{\"internalType\":\"contractIFrostWalletOwner\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"withdrawRewards\",\"outputs\":[{\"internalType\":\"uint96\",\"name\":\"\",\"type\":\"uint96\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x6101c06040523480156200001257600080fd5b5060405162005804380380620058048339810160408190526200003591620002af565b620000403362000200565b600180556001600160a01b038716158015906200006557506001600160a01b03861615155b80156200007a57506001600160a01b03851615155b80156200008f57506001600160a01b03841615155b8015620000a457506001600160a01b03831615155b620000e95760405162461bcd60e51b815260206004820152601060248201526f46524f5354207265666572656e63657360801b60448201526064015b60405180910390fd5b81516001600160401b031615801590620001175750604082015160208301516001600160401b039182169116115b801562000130575060408201516001600160401b031615155b801562000149575060608201516001600160401b031615155b80156200015a575060008161ffff16115b620001975760405162461bcd60e51b815260206004820152600c60248201526b46524f535420706f6c69637960a01b6044820152606401620000e0565b6001600160a01b0396871660805294861660a05292851660c05290841660e0529092166101005281516001600160401b0390811661012052602083015181166101405260408301518116610160526060909201519091166101805261ffff166101a052620003bd565b600080546001600160a01b038381166001600160a01b0319831681178455604051919092169283917f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e09190a35050565b6001600160a01b03811681146200026657600080fd5b50565b634e487b7160e01b600052604160045260246000fd5b80516001600160401b03811681146200029757600080fd5b919050565b805161ffff811681146200029757600080fd5b6000806000806000806000610140888a031215620002cc57600080fd5b8751620002d98162000250565b80975050602080890151620002ee8162000250565b60408a0151909750620003018162000250565b60608a0151909650620003148162000250565b60808a0151909550620003278162000250565b935060bf89018a136200033957600080fd5b604051608081016001600160401b03811182821017156200035e576200035e62000269565b604052806101208b018c8111156200037557600080fd5b60a08c015b818110156200039c576200038e816200027f565b83529184019184016200037a565b50829550620003ab816200029c565b94505050505092959891949750929550565b60805160a05160c05160e05161010051610120516101405161016051610180516101a05161529e62000566600039600081816102e30152611e6901526000818161045a0152613235015260008181610624015281816109c2015281816114740152612b200152600081816103a2015261103901526000818161031d01528181610dac01526119a30152600081816102750152818161110a0152818161202e0152818161263501526133a401526000818161023101528181610d4c01526113c60152600081816104b3015281816112390152611d2701526000818161034401528181610a6101528181610c9401528181610e8a015281816115b601528181611660015281816116ff0152818161179e015281816121e40152818161227401528181612c8a01528181612f2c015261386c01526000818161051c0152818161066201528181610a9401528181610cc301528181610e5b01528181610fa30152818161130701528181611aa801528181611db9015281816120c201528181612cc10152818161333101528181613603015281816136a10152818161370401526137d8015261529e6000f3fe608060405234801561001057600080fd5b50600436106102275760003560e01c80638b1bfba011610130578063c19d93fb116100b8578063cc6df4e81161007c578063cc6df4e8146105c3578063defeed9c146105e4578063e1759c3214610604578063f2fde38b1461060c578063f3f480d91461061f57600080fd5b8063c19d93fb1461053e578063c3b4b3c11461055d578063c7b8981c14610570578063ca9a22d114610590578063cada9f0d146105b057600080fd5b8063a59f185f116100ff578063a59f185f146104ae578063a963cfd0146104d5578063ac1676f0146104ea578063b2ce141c14610504578063b54a23741461051757600080fd5b80638b1bfba0146104555780638da5cb5b1461047c578063900cf0cf1461048d57806393647607146104a657600080fd5b80634755e25e116101b35780636febd464116101825780636febd464146103fc578063715018a61461040f57806372cc8c6d146104175780637e0049fd1461041f57806387721e1b1461043257600080fd5b80634755e25e1461039d5780635b10538c146103c45780635cc8ec61146103cc578063617a7a6f146103ef57600080fd5b8063239cf870116101fa578063239cf870146102de5780632730780d146103185780633a5381b51461033f578063449b2f4414610366578063474200051461038657600080fd5b8063153622b31461022c5780631ae879e8146102705780631bfe31db146102975780631ca10799146102ac575b600080fd5b6102537f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020015b60405180910390f35b6102537f000000000000000000000000000000000000000000000000000000000000000081565b6102aa6102a5366004613cae565b610646565b005b6002546102c690600160901b90046001600160401b031681565b6040516001600160401b039091168152602001610267565b6103057f000000000000000000000000000000000000000000000000000000000000000081565b60405161ffff9091168152602001610267565b6102c67f000000000000000000000000000000000000000000000000000000000000000081565b6102537f000000000000000000000000000000000000000000000000000000000000000081565b610379610374366004613cd2565b610778565b6040516102679190613ecf565b61038f60035481565b604051908152602001610267565b6102c67f000000000000000000000000000000000000000000000000000000000000000081565b6102aa610983565b6103df6103da366004613f16565b610c65565b6040519015158152602001610267565b6002546103df9060ff1681565b6102aa61040a366004613f63565b610d41565b6102aa6110e5565b6102aa6110f7565b6102aa61042d366004613f85565b61142b565b6103df610440366004613cd2565b60096020526000908152604090205460ff1681565b6102c67f000000000000000000000000000000000000000000000000000000000000000081565b6000546001600160a01b0316610253565b6002546102c6906201000090046001600160401b031681565b6102aa611964565b6102537f000000000000000000000000000000000000000000000000000000000000000081565b6104dd611b5f565b6040516102679190614103565b6002546102c690600160501b90046001600160401b031681565b6102aa61051236600461412b565b611ca6565b6102537f000000000000000000000000000000000000000000000000000000000000000081565b60025461055090610100900460ff1681565b6040516102679190614155565b6102aa61056b366004613cd2565b611f3d565b61057861209c565b6040516001600160601b039091168152602001610267565b61038f61059e366004614168565b600a6020526000908152604090205481565b6102aa6105be3660046141c7565b612145565b6105d66105d1366004614168565b6126ad565b604051610267929190614297565b6105f76105f2366004614168565b6127ad565b6040516102679190614321565b6102aa612ae1565b6102aa61061a36600461412b565b61346b565b6102c67f000000000000000000000000000000000000000000000000000000000000000081565b61064e6134e1565b8015806106ed5750306001600160a01b03167f00000000000000000000000000000000000000000000000000000000000000006001600160a01b0316638da5cb5b6040518163ffffffff1660e01b8152600401602060405180830381865afa1580156106be573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906106e291906143e9565b6001600160a01b0316145b6107315760405162461bcd60e51b815260206004820152601060248201526f232927a9aa103837b7b61037bbb732b960811b60448201526064015b60405180910390fd5b6002805460ff19168215159081179091556040519081527f829ccc110ba2242259f6b5b2d802c6cc474cb74808a64ab21a57460cee99bc289060200160405180910390a150565b610780613a7b565b600082815260086020908152604091829020825161020081018452815460ff80821660c084019081526101009283900490911660e084015260018401549183019190915260028301546001600160a01b038116610120840152600160a01b90046001600160401b031661014083015260038301805486518187028101870190975280875292959394869492938693610160870193909290919083018282801561087457602002820191906000526020600020906000905b82829054906101000a900463ffffffff1663ffffffff16815260200190600401906020826003010492830192600103820291508084116108375790505b50505050508152602001600482018054806020026020016040519081016040528092919081815260200182805480156108d657602002820191906000526020600020905b81546001600160a01b031681526001909101906020018083116108b8575b5050509183525050600582015461ffff166020808301919091526006830154604080840191909152600790930154606092830152928452600885015492840192909252600984015490830152600a8301546001600160401b0380821692840192909252600160401b8104909116608083015260a090910190600160801b900460ff16600381111561096957610969613e2e565b600381111561097a5761097a613e2e565b90525092915050565b61098b61353b565b6003600254610100900460ff1660038111156109a9576109a9613e2e565b1480156109f457506002546109f1906001600160401b037f0000000000000000000000000000000000000000000000000000000000000000811691600160901b90041661441c565b43105b610a395760405162461bcd60e51b8152602060048201526016602482015275119493d4d50818da185b1b195b99d94818db1bdcd95960521b6044820152606401610728565b6002546201000090046001600160401b031660008181526007602052604090206004805491927f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031692635694bcce9230927f00000000000000000000000000000000000000000000000000000000000000009290918791600183019190610ac79061442f565b80601f0160208091040260200160405190810160405280929190818152602001828054610af39061442f565b8015610b405780601f10610b1557610100808354040283529160200191610b40565b820191906000526020600020905b815481529060010190602001808311610b2357829003601f168201915b5050505050806020019051810190610b5891906146b3565b6040518763ffffffff1660e01b8152600401610b799695949392919061498c565b602060405180830381865afa158015610b96573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610bba91906149fa565b15610bfc5760405162461bcd60e51b8152602060048201526012602482015271119493d4d5081d985b1a59081c995cdd5b1d60721b6044820152606401610728565b600354600254604051620100009091046001600160401b0316907ff0668f1933e7ca9482369ea0ce991bb44e521611f75df986153f1606b6b129b790600090a3610c4860046000613b01565b5060006003556002805461ff00191661020017905560018055565b565b6001600160401b0381166000908152600760205260408082209051632b4a5e6760e11b81526001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001690635694bcce90610cf69030907f0000000000000000000000000000000000000000000000000000000000000000908890879060018201908c90600401614c36565b602060405180830381865afa158015610d13573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610d3791906149fa565b9150505b92915050565b336001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016148015610d9457506001600254610100900460ff166003811115610d9257610d92613e2e565b145b8015610ddd5750600254610dda906001600160401b037f00000000000000000000000000000000000000000000000000000000000000008116916201000090041661441c565b43105b610e1d5760405162461bcd60e51b8152602060048201526011602482015270119493d4d5081cd959590819195b9a5959607a1b6044820152606401610728565b60006007600060028054906101000a90046001600160401b03166001600160401b03166001600160401b0316815260200190815260200160002090507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b0316636c2530b97f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166363b635ea6040518163ffffffff1660e01b8152600401602060405180830381865afa158015610ee6573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610f0a9190614ca7565b60405160e083901b6001600160e01b031916815261ffff909116600482015260248101869052604401600060405180830381865afa158015610f50573d6000803e3d6000fd5b505050506040513d6000823e601f3d908101601f19168201604052610f789190810190614cc4565b8051610f8b918391602090910190613b3b565b50604051637bfcd47d60e11b81526001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000169063f7f9a8fa90610fd8908490600401614cf8565b600060405180830381865afa158015610ff5573d6000803e3d6000fd5b505050506040513d6000823e601f3d908101601f1916820160405261101d9190810190614d0b565b8051611033916001840191602090910190613bea565b5061105e7f000000000000000000000000000000000000000000000000000000000000000043614da4565b6002805461020071ffffffffffffffff0000000000000000ff001990911661ff0019600160501b6001600160401b03958616021617179081905560405162010000909104909116907f2c7168182f5ca9203996dfcc0bceedb1c0c61a3f3c165ec69473a7af9b16d444906110d89084906001820190614dcb565b60405180910390a2505050565b6110ed6134e1565b610c636000613594565b6110ff61353b565b336001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016148015611139575060025460ff165b61117c5760405162461bcd60e51b8152602060048201526014602482015273119493d4d5081c995c5d595cdd0819195b9a595960621b6044820152606401610728565b6000600254610100900460ff16600381111561119a5761119a613e2e565b1480156111b757506002546201000090046001600160401b031643115b6111f65760405162461bcd60e51b815260206004820152601060248201526f46524f535420444b472061637469766560801b6044820152606401610728565b60005b600554811015611304576112f26005828154811061121957611219614df0565b9060005260206000200160009054906101000a90046001600160a01b03167f00000000000000000000000000000000000000000000000000000000000000006001600160a01b0316637e33cba66005858154811061127957611279614df0565b60009182526020909120015460405160e083901b6001600160e01b03191681526001600160a01b039091166004820152602401602060405180830381865afa1580156112c9573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906112ed9190614e06565b6135e4565b806112fc81614e2f565b9150506111f9565b507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663f83d08ba6040518163ffffffff1660e01b8152600401600060405180830381600087803b15801561136057600080fd5b505af1158015611374573d6000803e3d6000fd5b50506002805461010061ff00196001600160401b03431662010000021669ffffffffffffffffff00199092169190911717905550506040516310da3b0360e21b81523060048201526001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001690634368ec0c90602401600060405180830381600087803b15801561140a57600080fd5b505af115801561141e573d6000803e3d6000fd5b50505050610c6360018055565b61143361353b565b60028054610100900460ff16600381111561145057611450613e2e565b14801561149c57506002546001600160401b03600160501b90910481169061149a907f0000000000000000000000000000000000000000000000000000000000000000164361441c565b105b6114e85760405162461bcd60e51b815260206004820152601760248201527f46524f5354207375626d697373696f6e20636c6f7365640000000000000000006044820152606401610728565b6002546201000090046001600160401b0316600090815260076020526040902081351580159061151d57506001810154823511155b801561155d575033600180830190611536908535614e48565b8154811061154657611546614df0565b6000918252602090912001546001600160a01b0316145b61159b5760405162461bcd60e51b815260206004820152600f60248201526e232927a9aa1039bab136b4ba3a32b960891b6044820152606401610728565b6115a86020830183614e5b565b9050604114801561165757507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166363b635ea6040518163ffffffff1660e01b8152600401602060405180830381865afa158015611612573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906116369190614ca7565b6116459061ffff166041614ea1565b6116526060840184614e5b565b905011155b80156116f657507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166363b635ea6040518163ffffffff1660e01b8152600401602060405180830381865afa1580156116bc573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906116e09190614ca7565b61ffff166116f160a0840184614eb8565b905011155b801561179557507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166363b635ea6040518163ffffffff1660e01b8152600401602060405180830381865afa15801561175b573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061177f9190614ca7565b61ffff166117906080840184614eb8565b905011155b801561183457507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166363b635ea6040518163ffffffff1660e01b8152600401602060405180830381865afa1580156117fa573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061181e9190614ca7565b61ffff1661182f6040840184614eb8565b905011155b6118765760405162461bcd60e51b815260206004820152601360248201527246524f535420726573756c7420626f756e647360681b6044820152606401610728565b816040516020016118879190614f01565b604051602081830303815290604052600490816118a49190614f5f565b5060046040516118b4919061501e565b6040519081900390206003908155600280546001600160401b034316600160901b0267ffffffffffffffff60901b1982168117835561ff00191679ffffffffffffffff00000000000000000000000000000000ff001990911617610100830217905550600354600254604051620100009091046001600160401b0316907f63981ff2f400f06b7afcec41b56f315cf037a8ec4c186c807ddd0806c292beda90600090a35061196160018055565b50565b61196c61353b565b6001600254610100900460ff16600381111561198a5761198a613e2e565b1480156119d557506002546119d1906001600160401b037f00000000000000000000000000000000000000000000000000000000000000008116916201000090041661441c565b4310155b80611a3d575060028054610100900460ff1660038111156119f8576119f8613e2e565b1480611a1f57506003600254610100900460ff166003811115611a1d57611a1d613e2e565b145b8015611a3d5750600254600160501b90046001600160401b03164310155b611a895760405162461bcd60e51b815260206004820152601760248201527f46524f535420444b47206578706972792064656e6965640000000000000000006044820152606401610728565b6002805461ff0019169055611aa060046000613b01565b6003600090557f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663a69df4b56040518163ffffffff1660e01b8152600401600060405180830381600087803b158015611b0157600080fd5b505af1158015611b15573d6000803e3d6000fd5b5050600254604051620100009091046001600160401b031692507f60a3d87e581920b0cb996294ea71423f65e9ea7c454087f5b220d7ac5d03798b9150600090a2610c6360018055565b611ba26040518060e00160405280600081526020016060815260200160608152602001606081526020016060815260200160608152602001600080191681525090565b6003600254610100900460ff166003811115611bc057611bc0613e2e565b14611c035760405162461bcd60e51b8152602060048201526013602482015272232927a9aa1037379039bab136b4b9b9b4b7b760691b6044820152606401610728565b60048054611c109061442f565b80601f0160208091040260200160405190810160405280929190818152602001828054611c3c9061442f565b8015611c895780601f10611c5e57610100808354040283529160200191611c89565b820191906000526020600020905b815481529060010190602001808311611c6c57829003601f168201915b5050505050806020019051810190611ca191906146b3565b905090565b6000600254610100900460ff166003811115611cc457611cc4613e2e565b14611d055760405162461bcd60e51b8152602060048201526011602482015270119493d4d5081c1bdbdb081b1bd8dad959607a1b6044820152606401610728565b604051633f19e5d360e11b81526001600160a01b0382811660048301526000917f000000000000000000000000000000000000000000000000000000000000000090911690637e33cba690602401602060405180830381865afa158015611d70573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190611d949190614e06565b6001600160a01b03831660009081526006602052604090205490915060ff16611f2f577f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166343a3db306040518163ffffffff1660e01b8152600401602060405180830381865afa158015611e15573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190611e399190615094565b816001600160601b031610158015611e5a57506000816001600160601b0316115b8015611e8b575060055461ffff7f000000000000000000000000000000000000000000000000000000000000000016115b611ec95760405162461bcd60e51b815260206004820152600f60248201526e232927a9aa1030b236b4b9b9b4b7b760891b6044820152606401610728565b6001600160a01b0382166000818152600660205260408120805460ff191660019081179091556005805491820181559091527f036b6384b5eca791c62761152d0c79bb0604c104a5fb6f4eb0703f3154bb3db00180546001600160a01b03191690911790555b611f3982826135e4565b5050565b611f4561353b565b60008181526008602052604090206001600a820154600160801b900460ff166003811115611f7557611f75613e2e565b148015611f965750600a810154600160401b90046001600160401b03164310155b611fd85760405162461bcd60e51b8152602060048201526013602482015272119493d4d508195e1c1a5c9e4819195b9a5959606a1b6044820152606401610728565b600a8101805460ff60801b1916600360801b17905560405182907fc8a47d32f4f3c7278ceea67de863848fcd8bfc55423405c24bc8f56bd36c79f190600090a2604051631b61d9fb60e21b8152600481018390527f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031690636d8767ec90602401600060405180830381600087803b15801561207a57600080fd5b505af115801561208e573d6000803e3d6000fd5b505050505061196160018055565b60006120a661353b565b604051637104c0e560e11b8152336004820181905260248201527f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03169063e20981ca906044016020604051808303816000875af1158015612113573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906121379190614e06565b905061214260018055565b90565b61214d61353b565b60008781526008602052604090206001600a820154600160801b900460ff16600381111561217d5761217d613e2e565b14801561219d5750600a810154600160401b90046001600160401b031643105b6121e25760405162461bcd60e51b8152602060048201526016602482015275119493d4d5081c9958591a5b995cdcc818db1bdcd95960521b6044820152606401610728565b7f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663bfac640e6040518163ffffffff1660e01b8152600401602060405180830381865afa158015612240573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906122649190614ca7565b61ffff1686108015906122fc57507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166363b635ea6040518163ffffffff1660e01b8152600401602060405180830381865afa1580156122d0573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906122f49190614ca7565b61ffff168611155b801561230757508386145b801561231c5750612319866041614ea1565b82145b6123605760405162461bcd60e51b8152602060048201526015602482015274119493d4d5081c9958591a5b995cdcc818dbdd5b9d605a1b6044820152606401610728565b6000805b878110156125ac57600089898381811061238057612380614df0565b905060200201602081019061239591906150ad565b90508261ffff168161ffff161180156123b65750600484015461ffff821611155b80156123db575060008888848181106123d1576123d1614df0565b9050602002013514155b61241e5760405162461bcd60e51b8152602060048201526014602482015273119493d4d5081c9958591a5b995cdcc81cd9585d60621b6044820152606401610728565b8092506000612447858d848c8c8881811061243b5761243b614df0565b90506020020135613807565b90506000806124f1612486847f19457468657265756d205369676e6564204d6573736167653a0a3332000000006000908152601c91909152603c902090565b8a8a612493896041614ea1565b9061249f8a600161441c565b6124aa906041614ea1565b926124b7939291906150ca565b8080601f0160208091040260200160405190810160405280939291908181526020018383808284376000920191909152506138e992505050565b9092509050600081600481111561250a5761250a613e2e565b1480156125505750600487016125216001866150f4565b61ffff168154811061253557612535614df0565b6000918252602090912001546001600160a01b038381169116145b6125955760405162461bcd60e51b8152602060048201526016602482015275232927a9aa103932b0b234b732b9b99039b4b3b732b960511b6044820152606401610728565b5050505080806125a490614e2f565b915050612364565b50600a8201805460ff60801b1916600160811b17905560098201546040518a917f68bbc0cf00d2a6597f922a1160e608d4832cc29fb288d82470a5fd0f8885c0ca916125fc91908c908c9061510f565b60405180910390a260098201546040516370652dc360e11b8152600481018b9052602481019190915260016044820181905260648201527f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03169063e0ca5b8690608401600060405180830381600087803b15801561268157600080fd5b505af1158015612695573d6000803e3d6000fd5b5050505050506126a460018055565b50505050505050565b6001600160401b038116600090815260076020908152604091829020805483518184028101840190945280845260609384936001840192849183018282801561274157602002820191906000526020600020906000905b82829054906101000a900463ffffffff1663ffffffff16815260200190600401906020826003010492830192600103820291508084116127045790505b505050505091508080548060200260200160405190810160405280929190818152602001828054801561279d57602002820191906000526020600020905b81546001600160a01b0316815260019091019060200180831161277f575b5050505050905091509150915091565b6127b5613c3f565b6001600160401b038281166000908152600a6020908152604091829020548251610120810190935243909316825260025490820190610100900460ff16600381111561280357612803613e2e565b81526002546001600160401b0362010000820481166020840152600160901b820481166040840152600160501b90910416606082015260035460808201526004805460a0909201916128549061442f565b80601f01602080910402602001604051908101604052809291908181526020018280546128809061442f565b80156128cd5780601f106128a2576101008083540402835291602001916128cd565b820191906000526020600020905b8154815290600101906020018083116128b057829003601f168201915b50505091835250506020808201849052600084815260088252604090819020815161020081018352815460ff80821660c084019081526101009283900490911660e084015260018401549183019190915260028301546001600160a01b038116610120840152600160a01b90046001600160401b0316610140830152600383018054855181880281018801875281815295909701969295939486949293869361016087019391908301828280156129cf57602002820191906000526020600020906000905b82829054906101000a900463ffffffff1663ffffffff16815260200190600401906020826003010492830192600103820291508084116129925790505b5050505050815260200160048201805480602002602001604051908101604052809291908181526020018280548015612a3157602002820191906000526020600020905b81546001600160a01b03168152600190910190602001808311612a13575b5050509183525050600582015461ffff166020808301919091526006830154604080840191909152600790930154606092830152928452600885015492840192909252600984015490830152600a8301546001600160401b0380821692840192909252600160401b8104909116608083015260a090910190600160801b900460ff166003811115612ac457612ac4613e2e565b6003811115612ad557612ad5613e2e565b90525090529392505050565b612ae961353b565b6003600254610100900460ff166003811115612b0757612b07613e2e565b148015612b535750600254612b4f906001600160401b037f0000000000000000000000000000000000000000000000000000000000000000811691600160901b90041661441c565b4310155b8015612b705750600254600160501b90046001600160401b031643105b612bb45760405162461bcd60e51b8152602060048201526015602482015274119493d4d508185c1c1c9bdd985b0818db1bdcd959605a1b6044820152606401610728565b6002546201000090046001600160401b0316600090815260076020526040812060048054919291612be49061442f565b80601f0160208091040260200160405190810160405280929190818152602001828054612c109061442f565b8015612c5d5780601f10612c3257610100808354040283529160200191612c5d565b820191906000526020600020905b815481529060010190602001808311612c4057829003601f168201915b5050505050806020019051810190612c7591906146b3565b600254604051632b4a5e6760e11b81529192507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031691635694bcce91612d039130917f000000000000000000000000000000000000000000000000000000000000000091620100009091046001600160401b03169088906001820190899060040161498c565b602060405180830381865afa158015612d20573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190612d4491906149fa565b612d875760405162461bcd60e51b8152602060048201526014602482015273119493d4d5081a5b9d985b1a59081c995cdd5b1d60621b6044820152606401610728565b60208082015160218101516041820151600082815260099094526040909320549192909160ff1615612df15760405162461bcd60e51b815260206004820152601360248201527246524f5354206475706c6963617465206b657960681b6044820152606401610728565b6000612dfc8361392e565b60008181526008602090815260409182902082516101408101845260028082526001828501524682860152306060830152546201000090046001600160401b031660808201528a548451818502810185019095528085529495509093909260a084019290918b91830182828015612ebe57602002820191906000526020600020906000905b82829054906101000a900463ffffffff1663ffffffff1681526020019060040190602082600301049283019260010382029150808411612e815790505b5050505050815260200188600101805480602002602001604051908101604052809291908181526020018280548015612f2057602002820191906000526020600020905b81546001600160a01b03168152600190910190602001808311612f02575b505050505081526020017f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166342cde4e86040518163ffffffff1660e01b8152600401602060405180830381865afa158015612f88573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190612fac9190614ca7565b61ffff16815260208082018690526040918201879052825184548483015160ff9081166101000261ffff19909216921691909117178455908201516001840155606082015160028401805460808501516001600160401b0316600160a01b026001600160e01b03199091166001600160a01b039093169290921791909117905560a082015180518492613046926003850192910190613b3b565b5060c08201518051613062916004840191602090910190613bea565b5060e082015160058201805461ffff191661ffff909216919091179055610100808301516006830155610120909201516007909101556040805161014081018252835460ff808216835293900490921660208084019190915260018401548383015260028401546001600160a01b0381166060850152600160a01b90046001600160401b031660808401526003840180548351818402810184019094528084526131ff9493869360a086019391929083018282801561316c57602002820191906000526020600020906000905b82829054906101000a900463ffffffff1663ffffffff168152602001906004019060208260030104928301926001038202915080841161312f5790505b50505050508152602001600482018054806020026020016040519081016040528092919081815260200182805480156131ce57602002820191906000526020600020905b81546001600160a01b031681526001909101906020018083116131b0575b5050509183525050600582015461ffff16602082015260068201546040820152600790910154606090910152613982565b60098201556003546008820155600a8101805467ffffffffffffffff1916436001600160401b0381169190911790915561325a907f000000000000000000000000000000000000000000000000000000000000000090614da4565b600a8281018054600160801b70ffffffffffffffffff00000000000000001990911660ff60801b19600160401b6001600160401b039687160216171790556000868152600960208181526040808420805460ff191660011790556002805462010000908190048816865295835293819020889055600354935492870154905190815287959394909204909216917f26fbad10f980b75efec6b12d8e4cb35026d4c519f9abe33ad667169b366b0729910160405180910390a46002805461ff001916905561332960046000613b01565b6003600090557f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663a69df4b56040518163ffffffff1660e01b8152600401600060405180830381600087803b15801561338a57600080fd5b505af115801561339e573d6000803e3d6000fd5b505050507f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166353a8a48d826000016040516020016133e59190615162565b60405160208183030381529060405283600a0160089054906101000a90046001600160401b03166040518363ffffffff1660e01b8152600401613429929190615224565b600060405180830381600087803b15801561344357600080fd5b505af1158015613457573d6000803e3d6000fd5b5050505050505050505050610c6360018055565b6134736134e1565b6001600160a01b0381166134d85760405162461bcd60e51b815260206004820152602660248201527f4f776e61626c653a206e6577206f776e657220697320746865207a65726f206160448201526564647265737360d01b6064820152608401610728565b61196181613594565b6000546001600160a01b03163314610c635760405162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e65726044820152606401610728565b60026001540361358d5760405162461bcd60e51b815260206004820152601f60248201527f5265656e7472616e637947756172643a207265656e7472616e742063616c6c006044820152606401610728565b6002600155565b600080546001600160a01b038381166001600160a01b0319831681178455604051919092169283917f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e09190a35050565b6040516307b8c36760e51b81526001600160a01b0383811660048301527f0000000000000000000000000000000000000000000000000000000000000000169063f7186ce090602401602060405180830381865afa15801561364a573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061366e91906149fa565b156137025760405163dc7520c560e01b81526001600160a01b0383811660048301526001600160601b03831660248301527f0000000000000000000000000000000000000000000000000000000000000000169063dc7520c5906044015b600060405180830381600087803b1580156136e657600080fd5b505af11580156136fa573d6000803e3d6000fd5b505050505050565b7f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03166343a3db306040518163ffffffff1660e01b8152600401602060405180830381865afa158015613760573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906137849190615094565b816001600160601b0316101580156137a557506000816001600160601b0316115b15611f3957604051630483483160e31b81526001600160a01b0383811660048301526001600160601b03831660248301527f0000000000000000000000000000000000000000000000000000000000000000169063241a4188906044016136cc565b60098401546002850154604051633d0a65e560e01b8152600481018690526024810192909252306044830152600160a01b90046001600160401b0316606482015260016084820181905260a482015261ffff831660c482015260e481018290526000907f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031690633d0a65e59061010401602060405180830381865afa1580156138bc573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906138e09190615094565b95945050505050565b600080825160410361391f5760208301516040840151606085015160001a613913878285856139b7565b94509450505050613927565b506000905060025b9250929050565b604080517f672c8fff2a63d593d622fe5ed48c1dfab533cbecb0293cdff22f6b96b463039960208201529081018290526000906060015b604051602081830303815290604052805190602001209050919050565b60007ff1b530094264da0f32bf821ce2af1f9e74902d4a2e049c327c3ecdc07ea7fa208260405160200161396592919061524f565b6000807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a08311156139ee5750600090506003613a72565b6040805160008082526020820180845289905260ff881692820192909252606081018690526080810185905260019060a0016020604051602081039080840390855afa158015613a42573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b038116613a6b57600060019250925050613a72565b9150600090505b94509492505050565b6040805161020081018252600060c0820181815260e083018290526101008301829052610120830182905261014083018290526060610160840181905261018084018190526101a084018390526101c084018390526101e0840183905290835260208301829052928201819052918101829052608081018290529060a08201905b905290565b508054613b0d9061442f565b6000825580601f10613b1d575050565b601f0160209004906000526020600020908101906119619190613c8b565b82805482825590600052602060002090600701600890048101928215613bda5791602002820160005b83821115613ba857835183826101000a81548163ffffffff021916908363ffffffff1602179055509260200192600401602081600301049283019260010302613b64565b8015613bd85782816101000a81549063ffffffff0219169055600401602081600301049283019260010302613ba8565b505b50613be6929150613c8b565b5090565b828054828255906000526020600020908101928215613bda579160200282015b82811115613bda57825182546001600160a01b0319166001600160a01b03909116178255602090920191600190910190613c0a565b6040805161012081019091526000808252602082019081526000602082018190526040820181905260608083018290526080830182905260a083015260c082015260e001613afc613a7b565b5b80821115613be65760008155600101613c8c565b801515811461196157600080fd5b600060208284031215613cc057600080fd5b8135613ccb81613ca0565b9392505050565b600060208284031215613ce457600080fd5b5035919050565b600081518084526020808501945080840160005b83811015613d2157815163ffffffff1687529582019590820190600101613cff565b509495945050505050565b600081518084526020808501945080840160005b83811015613d215781516001600160a01b031687529582019590820190600101613d40565b805160ff16825260006101406020830151613d85602086018260ff169052565b50604083015160408501526060830151613daa60608601826001600160a01b03169052565b506080830151613dc560808601826001600160401b03169052565b5060a08301518160a0860152613ddd82860182613ceb565b91505060c083015184820360c0860152613df78282613d2c565b91505060e0830151613e0f60e086018261ffff169052565b5061010083810151908501526101209283015192909301919091525090565b634e487b7160e01b600052602160045260246000fd5b6004811061196157634e487b7160e01b600052602160045260246000fd5b6000815160c08452613e7760c0850182613d65565b9050602083015160208501526040830151604085015260608301516001600160401b038082166060870152806080860151166080870152505060a0830151613ebe81613e44565b8060a0860152508091505092915050565b602081526000613ccb6020830184613e62565b600060e08284031215613ef457600080fd5b50919050565b80356001600160401b0381168114613f1157600080fd5b919050565b60008060408385031215613f2957600080fd5b82356001600160401b03811115613f3f57600080fd5b613f4b85828601613ee2565b925050613f5a60208401613efa565b90509250929050565b60008060408385031215613f7657600080fd5b50508035926020909101359150565b600060208284031215613f9757600080fd5b81356001600160401b03811115613fad57600080fd5b610d3784828501613ee2565b60005b83811015613fd4578181015183820152602001613fbc565b50506000910152565b60008151808452613ff5816020860160208601613fb9565b601f01601f19169290920160200192915050565b600081518084526020808501945080840160005b83811015613d215781518752958201959082019060010161401d565b80518252600060208083015160e08286015261405860e0860182613fdd565b60408581015187830391880191909152805180835290840192506000918401905b8083101561409c57835160ff168252928401926001929092019190840190614079565b506060860151935086810360608801526140b68185613fdd565b9350505050608083015184820360808601526140d28282614009565b91505060a083015184820360a08601526140ec8282613ceb565b91505060c083015160c08501528091505092915050565b602081526000613ccb6020830184614039565b6001600160a01b038116811461196157600080fd5b60006020828403121561413d57600080fd5b8135613ccb81614116565b61415181613e44565b9052565b6020810161416283613e44565b91905290565b60006020828403121561417a57600080fd5b613ccb82613efa565b60008083601f84011261419557600080fd5b5081356001600160401b038111156141ac57600080fd5b6020830191508360208260051b850101111561392757600080fd5b60008060008060008060006080888a0312156141e257600080fd5b8735965060208801356001600160401b038082111561420057600080fd5b61420c8b838c01614183565b909850965060408a013591508082111561422557600080fd5b6142318b838c01614183565b909650945060608a013591508082111561424a57600080fd5b818a0191508a601f83011261425e57600080fd5b81358181111561426d57600080fd5b8b602082850101111561427f57600080fd5b60208301945080935050505092959891949750929550565b604080825283519082018190526000906020906060840190828701845b828110156142d657815163ffffffff16845292840192908401906001016142b4565b5050508381038285015284518082528583019183019060005b818110156143145783516001600160a01b0316835292840192918401916001016142ef565b5090979650505050505050565b6020815261433b6020820183516001600160401b03169052565b6000602083015161434f6040840182614148565b5060408301516001600160401b03811660608401525060608301516001600160401b03811660808401525060808301516001600160401b03811660a08401525060a083015160c083015260c08301516101208060e08501526143b5610140850183613fdd565b60e086015161010086810191909152860151858203601f1901838701529092506143df8382613e62565b9695505050505050565b6000602082840312156143fb57600080fd5b8151613ccb81614116565b634e487b7160e01b600052601160045260246000fd5b80820180821115610d3b57610d3b614406565b600181811c9082168061444357607f821691505b602082108103613ef457634e487b7160e01b600052602260045260246000fd5b634e487b7160e01b600052604160045260246000fd5b60405160e081016001600160401b038111828210171561449b5761449b614463565b60405290565b604051601f8201601f191681016001600160401b03811182821017156144c9576144c9614463565b604052919050565b600082601f8301126144e257600080fd5b81516001600160401b038111156144fb576144fb614463565b61450e601f8201601f19166020016144a1565b81815284602083860101111561452357600080fd5b614534826020830160208701613fb9565b949350505050565b60006001600160401b0382111561455557614555614463565b5060051b60200190565b60ff8116811461196157600080fd5b600082601f83011261457f57600080fd5b8151602061459461458f8361453c565b6144a1565b82815260059290921b840181019181810190868411156145b357600080fd5b8286015b848110156145d75780516145ca8161455f565b83529183019183016145b7565b509695505050505050565b600082601f8301126145f357600080fd5b8151602061460361458f8361453c565b82815260059290921b8401810191818101908684111561462257600080fd5b8286015b848110156145d75780518352918301918301614626565b63ffffffff8116811461196157600080fd5b600082601f83011261466057600080fd5b8151602061467061458f8361453c565b82815260059290921b8401810191818101908684111561468f57600080fd5b8286015b848110156145d75780516146a68161463d565b8352918301918301614693565b6000602082840312156146c557600080fd5b81516001600160401b03808211156146dc57600080fd5b9083019060e082860312156146f057600080fd5b6146f8614479565b8251815260208301518281111561470e57600080fd5b61471a878286016144d1565b60208301525060408301518281111561473257600080fd5b61473e8782860161456e565b60408301525060608301518281111561475657600080fd5b614762878286016144d1565b60608301525060808301518281111561477a57600080fd5b614786878286016145e2565b60808301525060a08301518281111561479e57600080fd5b6147aa8782860161464f565b60a08301525060c083015160c082015280935050505092915050565b805480835260008281526020808220940193909190825b8260078201101561485557815463ffffffff8082168852602082811c821690890152604082811c821690890152606082811c821690890152608082811c82169089015260a082811c82169089015260c082811c9091169088015260e090811c90870152610100909501946001909101906008016147dd565b905490828110156148735763ffffffff821686526020909501946001015b8281101561489357602082901c63ffffffff168652602095909501946001015b828110156148b25763ffffffff604083901c1686526020909501946001015b828110156148d15763ffffffff606083901c1686526020909501946001015b828110156148f05763ffffffff608083901c1686526020909501946001015b8281101561490f5763ffffffff60a083901c1686526020909501946001015b8281101561492e5763ffffffff60c083901c1686526020909501946001015b828110156149445760e082901c86526020860195505b5093949350505050565b6000815480845260208085019450836000528060002060005b83811015613d215781546001600160a01b031687529582019560019182019101614967565b6001600160a01b038781168252861660208201526001600160401b038516604082015260c0606082018190526000906149c7908301866147c6565b82810360808401526149d9818661494e565b905082810360a08401526149ed8185614039565b9998505050505050505050565b600060208284031215614a0c57600080fd5b8151613ccb81613ca0565b6000808335601e19843603018112614a2e57600080fd5b83016020810192503590506001600160401b03811115614a4d57600080fd5b80360382131561392757600080fd5b81835281816020850137506000828201602090810191909152601f909101601f19169091010190565b6000808335601e19843603018112614a9c57600080fd5b83016020810192503590506001600160401b03811115614abb57600080fd5b8060051b360382131561392757600080fd5b81835260006001600160fb1b03831115614ae657600080fd5b8260051b80836020870137939093016020019392505050565b8183526000602080850194508260005b85811015613d21578135614b228161463d565b63ffffffff1687529582019590820190600101614b0f565b8035825260006020614b4e81840184614a17565b60e083870152614b6260e087018284614a5c565b915050614b726040850185614a85565b868303604088015280835290916000919084015b81831015614bb0578335614b998161455f565b60ff16815292840192600192909201918401614b86565b614bbd6060880188614a17565b955093508781036060890152614bd4818686614a5c565b945050505050614be76080840184614a85565b8583036080870152614bfa838284614acd565b92505050614c0b60a0840184614a85565b85830360a0870152614c1e838284614aff565b9250505060c083013560c08501528091505092915050565b6001600160a01b038781168252861660208201526001600160401b038516604082015260c060608201819052600090614c71908301866147c6565b8281036080840152614c83818661494e565b905082810360a08401526149ed8185614b3a565b61ffff8116811461196157600080fd5b600060208284031215614cb957600080fd5b8151613ccb81614c97565b600060208284031215614cd657600080fd5b81516001600160401b03811115614cec57600080fd5b610d378482850161464f565b602081526000613ccb60208301846147c6565b60006020808385031215614d1e57600080fd5b82516001600160401b03811115614d3457600080fd5b8301601f81018513614d4557600080fd5b8051614d5361458f8261453c565b81815260059190911b82018301908381019087831115614d7257600080fd5b928401925b82841015614d99578351614d8a81614116565b82529284019290840190614d77565b979650505050505050565b6001600160401b03818116838216019080821115614dc457614dc4614406565b5092915050565b604081526000614dde60408301856147c6565b82810360208401526138e0818561494e565b634e487b7160e01b600052603260045260246000fd5b600060208284031215614e1857600080fd5b81516001600160601b0381168114613ccb57600080fd5b600060018201614e4157614e41614406565b5060010190565b81810381811115610d3b57610d3b614406565b6000808335601e19843603018112614e7257600080fd5b8301803591506001600160401b03821115614e8c57600080fd5b60200191503681900382131561392757600080fd5b8082028115828204841417610d3b57610d3b614406565b6000808335601e19843603018112614ecf57600080fd5b8301803591506001600160401b03821115614ee957600080fd5b6020019150600581901b360382131561392757600080fd5b602081526000613ccb6020830184614b3a565b601f821115614f5a57600081815260208120601f850160051c81016020861015614f3b5750805b601f850160051c820191505b818110156136fa57828155600101614f47565b505050565b81516001600160401b03811115614f7857614f78614463565b614f8c81614f86845461442f565b84614f14565b602080601f831160018114614fc15760008415614fa95750858301515b600019600386901b1c1916600185901b1785556136fa565b600085815260208120601f198616915b82811015614ff057888601518255948401946001909101908401614fd1565b508582101561500e5787850151600019600388901b60f8161c191681555b5050505050600190811b01905550565b600080835461502c8161442f565b60018281168015615044576001811461505957615088565b60ff1984168752821515830287019450615088565b8760005260208060002060005b8581101561507f5781548a820152908401908201615066565b50505082870194505b50929695505050505050565b6000602082840312156150a657600080fd5b5051919050565b6000602082840312156150bf57600080fd5b8135613ccb81614c97565b600080858511156150da57600080fd5b838611156150e757600080fd5b5050820193919092039150565b61ffff828116828216039080821115614dc457614dc4614406565b83815260406020808301829052908201839052600090849060608401835b8681101561515657833561514081614c97565b61ffff168252928201929082019060010161512d565b50979650505050505050565b602081526000825461517c6020840160ff831660ff169052565b600881901c60ff166040840152506001830154606083015260028301546001600160a01b038116608084015260a081811c6001600160401b031690840152506101408060c08401526151d56101608401600386016147c6565b838103601f190160e08501526151ee816004870161494e565b90506151ff600586015461ffff1690565b61ffff1661010085015260068501546101208501526007909401549201919091525090565b6040815260006152376040830185613fdd565b90506001600160401b03831660208301529392505050565b8281526040602082015260006145346040830184613d6556fea2646970667358221220d2fb4070f79433562065dd9f66c2b6c9aa570cea2c7dd267353482dd652bf41164736f6c63430008110033",
}

// FrostWalletRegistryABI is the input ABI used to generate the binding from.
// Deprecated: Use FrostWalletRegistryMetaData.ABI instead.
var FrostWalletRegistryABI = FrostWalletRegistryMetaData.ABI

// FrostWalletRegistryBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use FrostWalletRegistryMetaData.Bin instead.
var FrostWalletRegistryBin = FrostWalletRegistryMetaData.Bin

// DeployFrostWalletRegistry deploys a new Ethereum contract, binding an instance of FrostWalletRegistry to it.
func DeployFrostWalletRegistry(auth *bind.TransactOpts, backend bind.ContractBackend, pool common.Address, policy common.Address, eligibility common.Address, beacon common.Address, bridge common.Address, periods [4]uint64, poolLimit uint16) (common.Address, *types.Transaction, *FrostWalletRegistry, error) {
	parsed, err := FrostWalletRegistryMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(FrostWalletRegistryBin), backend, pool, policy, eligibility, beacon, bridge, periods, poolLimit)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &FrostWalletRegistry{FrostWalletRegistryCaller: FrostWalletRegistryCaller{contract: contract}, FrostWalletRegistryTransactor: FrostWalletRegistryTransactor{contract: contract}, FrostWalletRegistryFilterer: FrostWalletRegistryFilterer{contract: contract}}, nil
}

// FrostWalletRegistry is an auto generated Go binding around an Ethereum contract.
type FrostWalletRegistry struct {
	FrostWalletRegistryCaller     // Read-only binding to the contract
	FrostWalletRegistryTransactor // Write-only binding to the contract
	FrostWalletRegistryFilterer   // Log filterer for contract events
}

// FrostWalletRegistryCaller is an auto generated read-only Go binding around an Ethereum contract.
type FrostWalletRegistryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostWalletRegistryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FrostWalletRegistryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostWalletRegistryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FrostWalletRegistryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FrostWalletRegistrySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FrostWalletRegistrySession struct {
	Contract     *FrostWalletRegistry // Generic contract binding to set the session for
	CallOpts     bind.CallOpts        // Call options to use throughout this session
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// FrostWalletRegistryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FrostWalletRegistryCallerSession struct {
	Contract *FrostWalletRegistryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts              // Call options to use throughout this session
}

// FrostWalletRegistryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FrostWalletRegistryTransactorSession struct {
	Contract     *FrostWalletRegistryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts              // Transaction auth options to use throughout this session
}

// FrostWalletRegistryRaw is an auto generated low-level Go binding around an Ethereum contract.
type FrostWalletRegistryRaw struct {
	Contract *FrostWalletRegistry // Generic contract binding to access the raw methods on
}

// FrostWalletRegistryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FrostWalletRegistryCallerRaw struct {
	Contract *FrostWalletRegistryCaller // Generic read-only contract binding to access the raw methods on
}

// FrostWalletRegistryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FrostWalletRegistryTransactorRaw struct {
	Contract *FrostWalletRegistryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFrostWalletRegistry creates a new instance of FrostWalletRegistry, bound to a specific deployed contract.
func NewFrostWalletRegistry(address common.Address, backend bind.ContractBackend) (*FrostWalletRegistry, error) {
	contract, err := bindFrostWalletRegistry(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistry{FrostWalletRegistryCaller: FrostWalletRegistryCaller{contract: contract}, FrostWalletRegistryTransactor: FrostWalletRegistryTransactor{contract: contract}, FrostWalletRegistryFilterer: FrostWalletRegistryFilterer{contract: contract}}, nil
}

// NewFrostWalletRegistryCaller creates a new read-only instance of FrostWalletRegistry, bound to a specific deployed contract.
func NewFrostWalletRegistryCaller(address common.Address, caller bind.ContractCaller) (*FrostWalletRegistryCaller, error) {
	contract, err := bindFrostWalletRegistry(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryCaller{contract: contract}, nil
}

// NewFrostWalletRegistryTransactor creates a new write-only instance of FrostWalletRegistry, bound to a specific deployed contract.
func NewFrostWalletRegistryTransactor(address common.Address, transactor bind.ContractTransactor) (*FrostWalletRegistryTransactor, error) {
	contract, err := bindFrostWalletRegistry(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryTransactor{contract: contract}, nil
}

// NewFrostWalletRegistryFilterer creates a new log filterer instance of FrostWalletRegistry, bound to a specific deployed contract.
func NewFrostWalletRegistryFilterer(address common.Address, filterer bind.ContractFilterer) (*FrostWalletRegistryFilterer, error) {
	contract, err := bindFrostWalletRegistry(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryFilterer{contract: contract}, nil
}

// bindFrostWalletRegistry binds a generic wrapper to an already deployed contract.
func bindFrostWalletRegistry(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FrostWalletRegistryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FrostWalletRegistry *FrostWalletRegistryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FrostWalletRegistry.Contract.FrostWalletRegistryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FrostWalletRegistry *FrostWalletRegistryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.FrostWalletRegistryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FrostWalletRegistry *FrostWalletRegistryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.FrostWalletRegistryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FrostWalletRegistry *FrostWalletRegistryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FrostWalletRegistry.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FrostWalletRegistry *FrostWalletRegistryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FrostWalletRegistry *FrostWalletRegistryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.contract.Transact(opts, method, params...)
}

// Admission is a free data retrieval call binding the contract method 0xa59f185f.
//
// Solidity: function admission() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) Admission(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "admission")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Admission is a free data retrieval call binding the contract method 0xa59f185f.
//
// Solidity: function admission() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistrySession) Admission() (common.Address, error) {
	return _FrostWalletRegistry.Contract.Admission(&_FrostWalletRegistry.CallOpts)
}

// Admission is a free data retrieval call binding the contract method 0xa59f185f.
//
// Solidity: function admission() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) Admission() (common.Address, error) {
	return _FrostWalletRegistry.Contract.Admission(&_FrostWalletRegistry.CallOpts)
}

// ApprovedWallet is a free data retrieval call binding the contract method 0xca9a22d1.
//
// Solidity: function approvedWallet(uint64 ) view returns(bytes32)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) ApprovedWallet(opts *bind.CallOpts, arg0 uint64) ([32]byte, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "approvedWallet", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ApprovedWallet is a free data retrieval call binding the contract method 0xca9a22d1.
//
// Solidity: function approvedWallet(uint64 ) view returns(bytes32)
func (_FrostWalletRegistry *FrostWalletRegistrySession) ApprovedWallet(arg0 uint64) ([32]byte, error) {
	return _FrostWalletRegistry.Contract.ApprovedWallet(&_FrostWalletRegistry.CallOpts, arg0)
}

// ApprovedWallet is a free data retrieval call binding the contract method 0xca9a22d1.
//
// Solidity: function approvedWallet(uint64 ) view returns(bytes32)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) ApprovedWallet(arg0 uint64) ([32]byte, error) {
	return _FrostWalletRegistry.Contract.ApprovedWallet(&_FrostWalletRegistry.CallOpts, arg0)
}

// ChallengePeriod is a free data retrieval call binding the contract method 0xf3f480d9.
//
// Solidity: function challengePeriod() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) ChallengePeriod(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "challengePeriod")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// ChallengePeriod is a free data retrieval call binding the contract method 0xf3f480d9.
//
// Solidity: function challengePeriod() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) ChallengePeriod() (uint64, error) {
	return _FrostWalletRegistry.Contract.ChallengePeriod(&_FrostWalletRegistry.CallOpts)
}

// ChallengePeriod is a free data retrieval call binding the contract method 0xf3f480d9.
//
// Solidity: function challengePeriod() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) ChallengePeriod() (uint64, error) {
	return _FrostWalletRegistry.Contract.ChallengePeriod(&_FrostWalletRegistry.CallOpts)
}

// Epoch is a free data retrieval call binding the contract method 0x900cf0cf.
//
// Solidity: function epoch() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) Epoch(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "epoch")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// Epoch is a free data retrieval call binding the contract method 0x900cf0cf.
//
// Solidity: function epoch() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) Epoch() (uint64, error) {
	return _FrostWalletRegistry.Contract.Epoch(&_FrostWalletRegistry.CallOpts)
}

// Epoch is a free data retrieval call binding the contract method 0x900cf0cf.
//
// Solidity: function epoch() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) Epoch() (uint64, error) {
	return _FrostWalletRegistry.Contract.Epoch(&_FrostWalletRegistry.CallOpts)
}

// EpochView is a free data retrieval call binding the contract method 0xdefeed9c.
//
// Solidity: function epochView(uint64 startBlock) view returns((uint64,uint8,uint64,uint64,uint64,bytes32,bytes,bytes32,((uint8,uint8,uint256,address,uint64,uint32[],address[],uint16,bytes32,bytes32),bytes32,bytes32,uint64,uint64,uint8)))
func (_FrostWalletRegistry *FrostWalletRegistryCaller) EpochView(opts *bind.CallOpts, startBlock uint64) (FrostWalletRegistryEpochSnapshot, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "epochView", startBlock)

	if err != nil {
		return *new(FrostWalletRegistryEpochSnapshot), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostWalletRegistryEpochSnapshot)).(*FrostWalletRegistryEpochSnapshot)

	return out0, err

}

// EpochView is a free data retrieval call binding the contract method 0xdefeed9c.
//
// Solidity: function epochView(uint64 startBlock) view returns((uint64,uint8,uint64,uint64,uint64,bytes32,bytes,bytes32,((uint8,uint8,uint256,address,uint64,uint32[],address[],uint16,bytes32,bytes32),bytes32,bytes32,uint64,uint64,uint8)))
func (_FrostWalletRegistry *FrostWalletRegistrySession) EpochView(startBlock uint64) (FrostWalletRegistryEpochSnapshot, error) {
	return _FrostWalletRegistry.Contract.EpochView(&_FrostWalletRegistry.CallOpts, startBlock)
}

// EpochView is a free data retrieval call binding the contract method 0xdefeed9c.
//
// Solidity: function epochView(uint64 startBlock) view returns((uint64,uint8,uint64,uint64,uint64,bytes32,bytes,bytes32,((uint8,uint8,uint256,address,uint64,uint32[],address[],uint16,bytes32,bytes32),bytes32,bytes32,uint64,uint64,uint8)))
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) EpochView(startBlock uint64) (FrostWalletRegistryEpochSnapshot, error) {
	return _FrostWalletRegistry.Contract.EpochView(&_FrostWalletRegistry.CallOpts, startBlock)
}

// IsResultValid is a free data retrieval call binding the contract method 0x5cc8ec61.
//
// Solidity: function isResultValid((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result, uint64 startBlock) view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) IsResultValid(opts *bind.CallOpts, result FrostDkgValidatorResult, startBlock uint64) (bool, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "isResultValid", result, startBlock)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsResultValid is a free data retrieval call binding the contract method 0x5cc8ec61.
//
// Solidity: function isResultValid((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result, uint64 startBlock) view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistrySession) IsResultValid(result FrostDkgValidatorResult, startBlock uint64) (bool, error) {
	return _FrostWalletRegistry.Contract.IsResultValid(&_FrostWalletRegistry.CallOpts, result, startBlock)
}

// IsResultValid is a free data retrieval call binding the contract method 0x5cc8ec61.
//
// Solidity: function isResultValid((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result, uint64 startBlock) view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) IsResultValid(result FrostDkgValidatorResult, startBlock uint64) (bool, error) {
	return _FrostWalletRegistry.Contract.IsResultValid(&_FrostWalletRegistry.CallOpts, result, startBlock)
}

// MaxPoolOperators is a free data retrieval call binding the contract method 0x239cf870.
//
// Solidity: function maxPoolOperators() view returns(uint16)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) MaxPoolOperators(opts *bind.CallOpts) (uint16, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "maxPoolOperators")

	if err != nil {
		return *new(uint16), err
	}

	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)

	return out0, err

}

// MaxPoolOperators is a free data retrieval call binding the contract method 0x239cf870.
//
// Solidity: function maxPoolOperators() view returns(uint16)
func (_FrostWalletRegistry *FrostWalletRegistrySession) MaxPoolOperators() (uint16, error) {
	return _FrostWalletRegistry.Contract.MaxPoolOperators(&_FrostWalletRegistry.CallOpts)
}

// MaxPoolOperators is a free data retrieval call binding the contract method 0x239cf870.
//
// Solidity: function maxPoolOperators() view returns(uint16)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) MaxPoolOperators() (uint16, error) {
	return _FrostWalletRegistry.Contract.MaxPoolOperators(&_FrostWalletRegistry.CallOpts)
}

// OutputKeyUsed is a free data retrieval call binding the contract method 0x87721e1b.
//
// Solidity: function outputKeyUsed(bytes32 ) view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) OutputKeyUsed(opts *bind.CallOpts, arg0 [32]byte) (bool, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "outputKeyUsed", arg0)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// OutputKeyUsed is a free data retrieval call binding the contract method 0x87721e1b.
//
// Solidity: function outputKeyUsed(bytes32 ) view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistrySession) OutputKeyUsed(arg0 [32]byte) (bool, error) {
	return _FrostWalletRegistry.Contract.OutputKeyUsed(&_FrostWalletRegistry.CallOpts, arg0)
}

// OutputKeyUsed is a free data retrieval call binding the contract method 0x87721e1b.
//
// Solidity: function outputKeyUsed(bytes32 ) view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) OutputKeyUsed(arg0 [32]byte) (bool, error) {
	return _FrostWalletRegistry.Contract.OutputKeyUsed(&_FrostWalletRegistry.CallOpts, arg0)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistrySession) Owner() (common.Address, error) {
	return _FrostWalletRegistry.Contract.Owner(&_FrostWalletRegistry.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) Owner() (common.Address, error) {
	return _FrostWalletRegistry.Contract.Owner(&_FrostWalletRegistry.CallOpts)
}

// RandomBeacon is a free data retrieval call binding the contract method 0x153622b3.
//
// Solidity: function randomBeacon() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) RandomBeacon(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "randomBeacon")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// RandomBeacon is a free data retrieval call binding the contract method 0x153622b3.
//
// Solidity: function randomBeacon() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistrySession) RandomBeacon() (common.Address, error) {
	return _FrostWalletRegistry.Contract.RandomBeacon(&_FrostWalletRegistry.CallOpts)
}

// RandomBeacon is a free data retrieval call binding the contract method 0x153622b3.
//
// Solidity: function randomBeacon() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) RandomBeacon() (common.Address, error) {
	return _FrostWalletRegistry.Contract.RandomBeacon(&_FrostWalletRegistry.CallOpts)
}

// ReadinessPeriod is a free data retrieval call binding the contract method 0x8b1bfba0.
//
// Solidity: function readinessPeriod() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) ReadinessPeriod(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "readinessPeriod")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// ReadinessPeriod is a free data retrieval call binding the contract method 0x8b1bfba0.
//
// Solidity: function readinessPeriod() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) ReadinessPeriod() (uint64, error) {
	return _FrostWalletRegistry.Contract.ReadinessPeriod(&_FrostWalletRegistry.CallOpts)
}

// ReadinessPeriod is a free data retrieval call binding the contract method 0x8b1bfba0.
//
// Solidity: function readinessPeriod() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) ReadinessPeriod() (uint64, error) {
	return _FrostWalletRegistry.Contract.ReadinessPeriod(&_FrostWalletRegistry.CallOpts)
}

// RequestsEnabled is a free data retrieval call binding the contract method 0x617a7a6f.
//
// Solidity: function requestsEnabled() view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) RequestsEnabled(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "requestsEnabled")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// RequestsEnabled is a free data retrieval call binding the contract method 0x617a7a6f.
//
// Solidity: function requestsEnabled() view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistrySession) RequestsEnabled() (bool, error) {
	return _FrostWalletRegistry.Contract.RequestsEnabled(&_FrostWalletRegistry.CallOpts)
}

// RequestsEnabled is a free data retrieval call binding the contract method 0x617a7a6f.
//
// Solidity: function requestsEnabled() view returns(bool)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) RequestsEnabled() (bool, error) {
	return _FrostWalletRegistry.Contract.RequestsEnabled(&_FrostWalletRegistry.CallOpts)
}

// ResultDeadline is a free data retrieval call binding the contract method 0xac1676f0.
//
// Solidity: function resultDeadline() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) ResultDeadline(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "resultDeadline")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// ResultDeadline is a free data retrieval call binding the contract method 0xac1676f0.
//
// Solidity: function resultDeadline() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) ResultDeadline() (uint64, error) {
	return _FrostWalletRegistry.Contract.ResultDeadline(&_FrostWalletRegistry.CallOpts)
}

// ResultDeadline is a free data retrieval call binding the contract method 0xac1676f0.
//
// Solidity: function resultDeadline() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) ResultDeadline() (uint64, error) {
	return _FrostWalletRegistry.Contract.ResultDeadline(&_FrostWalletRegistry.CallOpts)
}

// ResultTimeout is a free data retrieval call binding the contract method 0x4755e25e.
//
// Solidity: function resultTimeout() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) ResultTimeout(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "resultTimeout")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// ResultTimeout is a free data retrieval call binding the contract method 0x4755e25e.
//
// Solidity: function resultTimeout() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) ResultTimeout() (uint64, error) {
	return _FrostWalletRegistry.Contract.ResultTimeout(&_FrostWalletRegistry.CallOpts)
}

// ResultTimeout is a free data retrieval call binding the contract method 0x4755e25e.
//
// Solidity: function resultTimeout() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) ResultTimeout() (uint64, error) {
	return _FrostWalletRegistry.Contract.ResultTimeout(&_FrostWalletRegistry.CallOpts)
}

// SeedTimeout is a free data retrieval call binding the contract method 0x2730780d.
//
// Solidity: function seedTimeout() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) SeedTimeout(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "seedTimeout")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// SeedTimeout is a free data retrieval call binding the contract method 0x2730780d.
//
// Solidity: function seedTimeout() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) SeedTimeout() (uint64, error) {
	return _FrostWalletRegistry.Contract.SeedTimeout(&_FrostWalletRegistry.CallOpts)
}

// SeedTimeout is a free data retrieval call binding the contract method 0x2730780d.
//
// Solidity: function seedTimeout() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) SeedTimeout() (uint64, error) {
	return _FrostWalletRegistry.Contract.SeedTimeout(&_FrostWalletRegistry.CallOpts)
}

// Selection is a free data retrieval call binding the contract method 0xcc6df4e8.
//
// Solidity: function selection(uint64 startBlock) view returns(uint32[], address[])
func (_FrostWalletRegistry *FrostWalletRegistryCaller) Selection(opts *bind.CallOpts, startBlock uint64) ([]uint32, []common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "selection", startBlock)

	if err != nil {
		return *new([]uint32), *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]uint32)).(*[]uint32)
	out1 := *abi.ConvertType(out[1], new([]common.Address)).(*[]common.Address)

	return out0, out1, err

}

// Selection is a free data retrieval call binding the contract method 0xcc6df4e8.
//
// Solidity: function selection(uint64 startBlock) view returns(uint32[], address[])
func (_FrostWalletRegistry *FrostWalletRegistrySession) Selection(startBlock uint64) ([]uint32, []common.Address, error) {
	return _FrostWalletRegistry.Contract.Selection(&_FrostWalletRegistry.CallOpts, startBlock)
}

// Selection is a free data retrieval call binding the contract method 0xcc6df4e8.
//
// Solidity: function selection(uint64 startBlock) view returns(uint32[], address[])
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) Selection(startBlock uint64) ([]uint32, []common.Address, error) {
	return _FrostWalletRegistry.Contract.Selection(&_FrostWalletRegistry.CallOpts, startBlock)
}

// SortitionPool is a free data retrieval call binding the contract method 0xb54a2374.
//
// Solidity: function sortitionPool() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) SortitionPool(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "sortitionPool")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// SortitionPool is a free data retrieval call binding the contract method 0xb54a2374.
//
// Solidity: function sortitionPool() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistrySession) SortitionPool() (common.Address, error) {
	return _FrostWalletRegistry.Contract.SortitionPool(&_FrostWalletRegistry.CallOpts)
}

// SortitionPool is a free data retrieval call binding the contract method 0xb54a2374.
//
// Solidity: function sortitionPool() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) SortitionPool() (common.Address, error) {
	return _FrostWalletRegistry.Contract.SortitionPool(&_FrostWalletRegistry.CallOpts)
}

// State is a free data retrieval call binding the contract method 0xc19d93fb.
//
// Solidity: function state() view returns(uint8)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) State(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "state")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// State is a free data retrieval call binding the contract method 0xc19d93fb.
//
// Solidity: function state() view returns(uint8)
func (_FrostWalletRegistry *FrostWalletRegistrySession) State() (uint8, error) {
	return _FrostWalletRegistry.Contract.State(&_FrostWalletRegistry.CallOpts)
}

// State is a free data retrieval call binding the contract method 0xc19d93fb.
//
// Solidity: function state() view returns(uint8)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) State() (uint8, error) {
	return _FrostWalletRegistry.Contract.State(&_FrostWalletRegistry.CallOpts)
}

// SubmittedAt is a free data retrieval call binding the contract method 0x1ca10799.
//
// Solidity: function submittedAt() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) SubmittedAt(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "submittedAt")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// SubmittedAt is a free data retrieval call binding the contract method 0x1ca10799.
//
// Solidity: function submittedAt() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistrySession) SubmittedAt() (uint64, error) {
	return _FrostWalletRegistry.Contract.SubmittedAt(&_FrostWalletRegistry.CallOpts)
}

// SubmittedAt is a free data retrieval call binding the contract method 0x1ca10799.
//
// Solidity: function submittedAt() view returns(uint64)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) SubmittedAt() (uint64, error) {
	return _FrostWalletRegistry.Contract.SubmittedAt(&_FrostWalletRegistry.CallOpts)
}

// SubmittedHash is a free data retrieval call binding the contract method 0x47420005.
//
// Solidity: function submittedHash() view returns(bytes32)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) SubmittedHash(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "submittedHash")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// SubmittedHash is a free data retrieval call binding the contract method 0x47420005.
//
// Solidity: function submittedHash() view returns(bytes32)
func (_FrostWalletRegistry *FrostWalletRegistrySession) SubmittedHash() ([32]byte, error) {
	return _FrostWalletRegistry.Contract.SubmittedHash(&_FrostWalletRegistry.CallOpts)
}

// SubmittedHash is a free data retrieval call binding the contract method 0x47420005.
//
// Solidity: function submittedHash() view returns(bytes32)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) SubmittedHash() ([32]byte, error) {
	return _FrostWalletRegistry.Contract.SubmittedHash(&_FrostWalletRegistry.CallOpts)
}

// SubmittedResult is a free data retrieval call binding the contract method 0xa963cfd0.
//
// Solidity: function submittedResult() view returns((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32))
func (_FrostWalletRegistry *FrostWalletRegistryCaller) SubmittedResult(opts *bind.CallOpts) (FrostDkgValidatorResult, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "submittedResult")

	if err != nil {
		return *new(FrostDkgValidatorResult), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostDkgValidatorResult)).(*FrostDkgValidatorResult)

	return out0, err

}

// SubmittedResult is a free data retrieval call binding the contract method 0xa963cfd0.
//
// Solidity: function submittedResult() view returns((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32))
func (_FrostWalletRegistry *FrostWalletRegistrySession) SubmittedResult() (FrostDkgValidatorResult, error) {
	return _FrostWalletRegistry.Contract.SubmittedResult(&_FrostWalletRegistry.CallOpts)
}

// SubmittedResult is a free data retrieval call binding the contract method 0xa963cfd0.
//
// Solidity: function submittedResult() view returns((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32))
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) SubmittedResult() (FrostDkgValidatorResult, error) {
	return _FrostWalletRegistry.Contract.SubmittedResult(&_FrostWalletRegistry.CallOpts)
}

// Validator is a free data retrieval call binding the contract method 0x3a5381b5.
//
// Solidity: function validator() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) Validator(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "validator")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Validator is a free data retrieval call binding the contract method 0x3a5381b5.
//
// Solidity: function validator() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistrySession) Validator() (common.Address, error) {
	return _FrostWalletRegistry.Contract.Validator(&_FrostWalletRegistry.CallOpts)
}

// Validator is a free data retrieval call binding the contract method 0x3a5381b5.
//
// Solidity: function validator() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) Validator() (common.Address, error) {
	return _FrostWalletRegistry.Contract.Validator(&_FrostWalletRegistry.CallOpts)
}

// Wallet is a free data retrieval call binding the contract method 0x449b2f44.
//
// Solidity: function wallet(bytes32 id) view returns(((uint8,uint8,uint256,address,uint64,uint32[],address[],uint16,bytes32,bytes32),bytes32,bytes32,uint64,uint64,uint8))
func (_FrostWalletRegistry *FrostWalletRegistryCaller) Wallet(opts *bind.CallOpts, id [32]byte) (FrostWalletRegistryWallet, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "wallet", id)

	if err != nil {
		return *new(FrostWalletRegistryWallet), err
	}

	out0 := *abi.ConvertType(out[0], new(FrostWalletRegistryWallet)).(*FrostWalletRegistryWallet)

	return out0, err

}

// Wallet is a free data retrieval call binding the contract method 0x449b2f44.
//
// Solidity: function wallet(bytes32 id) view returns(((uint8,uint8,uint256,address,uint64,uint32[],address[],uint16,bytes32,bytes32),bytes32,bytes32,uint64,uint64,uint8))
func (_FrostWalletRegistry *FrostWalletRegistrySession) Wallet(id [32]byte) (FrostWalletRegistryWallet, error) {
	return _FrostWalletRegistry.Contract.Wallet(&_FrostWalletRegistry.CallOpts, id)
}

// Wallet is a free data retrieval call binding the contract method 0x449b2f44.
//
// Solidity: function wallet(bytes32 id) view returns(((uint8,uint8,uint256,address,uint64,uint32[],address[],uint16,bytes32,bytes32),bytes32,bytes32,uint64,uint64,uint8))
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) Wallet(id [32]byte) (FrostWalletRegistryWallet, error) {
	return _FrostWalletRegistry.Contract.Wallet(&_FrostWalletRegistry.CallOpts, id)
}

// WalletOwner is a free data retrieval call binding the contract method 0x1ae879e8.
//
// Solidity: function walletOwner() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCaller) WalletOwner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FrostWalletRegistry.contract.Call(opts, &out, "walletOwner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// WalletOwner is a free data retrieval call binding the contract method 0x1ae879e8.
//
// Solidity: function walletOwner() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistrySession) WalletOwner() (common.Address, error) {
	return _FrostWalletRegistry.Contract.WalletOwner(&_FrostWalletRegistry.CallOpts)
}

// WalletOwner is a free data retrieval call binding the contract method 0x1ae879e8.
//
// Solidity: function walletOwner() view returns(address)
func (_FrostWalletRegistry *FrostWalletRegistryCallerSession) WalletOwner() (common.Address, error) {
	return _FrostWalletRegistry.Contract.WalletOwner(&_FrostWalletRegistry.CallOpts)
}

// BeaconCallback is a paid mutator transaction binding the contract method 0x6febd464.
//
// Solidity: function __beaconCallback(uint256 seed, uint256 ) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) BeaconCallback(opts *bind.TransactOpts, seed *big.Int, arg1 *big.Int) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "__beaconCallback", seed, arg1)
}

// BeaconCallback is a paid mutator transaction binding the contract method 0x6febd464.
//
// Solidity: function __beaconCallback(uint256 seed, uint256 ) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) BeaconCallback(seed *big.Int, arg1 *big.Int) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.BeaconCallback(&_FrostWalletRegistry.TransactOpts, seed, arg1)
}

// BeaconCallback is a paid mutator transaction binding the contract method 0x6febd464.
//
// Solidity: function __beaconCallback(uint256 seed, uint256 ) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) BeaconCallback(seed *big.Int, arg1 *big.Int) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.BeaconCallback(&_FrostWalletRegistry.TransactOpts, seed, arg1)
}

// ApproveDkgResult is a paid mutator transaction binding the contract method 0xe1759c32.
//
// Solidity: function approveDkgResult() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) ApproveDkgResult(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "approveDkgResult")
}

// ApproveDkgResult is a paid mutator transaction binding the contract method 0xe1759c32.
//
// Solidity: function approveDkgResult() returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) ApproveDkgResult() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ApproveDkgResult(&_FrostWalletRegistry.TransactOpts)
}

// ApproveDkgResult is a paid mutator transaction binding the contract method 0xe1759c32.
//
// Solidity: function approveDkgResult() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) ApproveDkgResult() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ApproveDkgResult(&_FrostWalletRegistry.TransactOpts)
}

// ChallengeDkgResult is a paid mutator transaction binding the contract method 0x5b10538c.
//
// Solidity: function challengeDkgResult() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) ChallengeDkgResult(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "challengeDkgResult")
}

// ChallengeDkgResult is a paid mutator transaction binding the contract method 0x5b10538c.
//
// Solidity: function challengeDkgResult() returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) ChallengeDkgResult() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ChallengeDkgResult(&_FrostWalletRegistry.TransactOpts)
}

// ChallengeDkgResult is a paid mutator transaction binding the contract method 0x5b10538c.
//
// Solidity: function challengeDkgResult() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) ChallengeDkgResult() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ChallengeDkgResult(&_FrostWalletRegistry.TransactOpts)
}

// ExpireDkg is a paid mutator transaction binding the contract method 0x93647607.
//
// Solidity: function expireDkg() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) ExpireDkg(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "expireDkg")
}

// ExpireDkg is a paid mutator transaction binding the contract method 0x93647607.
//
// Solidity: function expireDkg() returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) ExpireDkg() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ExpireDkg(&_FrostWalletRegistry.TransactOpts)
}

// ExpireDkg is a paid mutator transaction binding the contract method 0x93647607.
//
// Solidity: function expireDkg() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) ExpireDkg() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ExpireDkg(&_FrostWalletRegistry.TransactOpts)
}

// ExpireWallet is a paid mutator transaction binding the contract method 0xc3b4b3c1.
//
// Solidity: function expireWallet(bytes32 id) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) ExpireWallet(opts *bind.TransactOpts, id [32]byte) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "expireWallet", id)
}

// ExpireWallet is a paid mutator transaction binding the contract method 0xc3b4b3c1.
//
// Solidity: function expireWallet(bytes32 id) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) ExpireWallet(id [32]byte) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ExpireWallet(&_FrostWalletRegistry.TransactOpts, id)
}

// ExpireWallet is a paid mutator transaction binding the contract method 0xc3b4b3c1.
//
// Solidity: function expireWallet(bytes32 id) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) ExpireWallet(id [32]byte) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.ExpireWallet(&_FrostWalletRegistry.TransactOpts, id)
}

// RefreshOperator is a paid mutator transaction binding the contract method 0xb2ce141c.
//
// Solidity: function refreshOperator(address operator) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) RefreshOperator(opts *bind.TransactOpts, operator common.Address) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "refreshOperator", operator)
}

// RefreshOperator is a paid mutator transaction binding the contract method 0xb2ce141c.
//
// Solidity: function refreshOperator(address operator) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) RefreshOperator(operator common.Address) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.RefreshOperator(&_FrostWalletRegistry.TransactOpts, operator)
}

// RefreshOperator is a paid mutator transaction binding the contract method 0xb2ce141c.
//
// Solidity: function refreshOperator(address operator) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) RefreshOperator(operator common.Address) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.RefreshOperator(&_FrostWalletRegistry.TransactOpts, operator)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) RenounceOwnership() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.RenounceOwnership(&_FrostWalletRegistry.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.RenounceOwnership(&_FrostWalletRegistry.TransactOpts)
}

// RequestNewWallet is a paid mutator transaction binding the contract method 0x72cc8c6d.
//
// Solidity: function requestNewWallet() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) RequestNewWallet(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "requestNewWallet")
}

// RequestNewWallet is a paid mutator transaction binding the contract method 0x72cc8c6d.
//
// Solidity: function requestNewWallet() returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) RequestNewWallet() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.RequestNewWallet(&_FrostWalletRegistry.TransactOpts)
}

// RequestNewWallet is a paid mutator transaction binding the contract method 0x72cc8c6d.
//
// Solidity: function requestNewWallet() returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) RequestNewWallet() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.RequestNewWallet(&_FrostWalletRegistry.TransactOpts)
}

// SetRequestsEnabled is a paid mutator transaction binding the contract method 0x1bfe31db.
//
// Solidity: function setRequestsEnabled(bool enabled) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) SetRequestsEnabled(opts *bind.TransactOpts, enabled bool) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "setRequestsEnabled", enabled)
}

// SetRequestsEnabled is a paid mutator transaction binding the contract method 0x1bfe31db.
//
// Solidity: function setRequestsEnabled(bool enabled) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) SetRequestsEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.SetRequestsEnabled(&_FrostWalletRegistry.TransactOpts, enabled)
}

// SetRequestsEnabled is a paid mutator transaction binding the contract method 0x1bfe31db.
//
// Solidity: function setRequestsEnabled(bool enabled) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) SetRequestsEnabled(enabled bool) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.SetRequestsEnabled(&_FrostWalletRegistry.TransactOpts, enabled)
}

// SubmitDkgResult is a paid mutator transaction binding the contract method 0x7e0049fd.
//
// Solidity: function submitDkgResult((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) SubmitDkgResult(opts *bind.TransactOpts, result FrostDkgValidatorResult) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "submitDkgResult", result)
}

// SubmitDkgResult is a paid mutator transaction binding the contract method 0x7e0049fd.
//
// Solidity: function submitDkgResult((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) SubmitDkgResult(result FrostDkgValidatorResult) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.SubmitDkgResult(&_FrostWalletRegistry.TransactOpts, result)
}

// SubmitDkgResult is a paid mutator transaction binding the contract method 0x7e0049fd.
//
// Solidity: function submitDkgResult((uint256,bytes,uint8[],bytes,uint256[],uint32[],bytes32) result) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) SubmitDkgResult(result FrostDkgValidatorResult) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.SubmitDkgResult(&_FrostWalletRegistry.TransactOpts, result)
}

// SubmitReadinessV1 is a paid mutator transaction binding the contract method 0xcada9f0d.
//
// Solidity: function submitReadinessV1(bytes32 id, uint16[] seats, bytes32[] references, bytes signatures) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) SubmitReadinessV1(opts *bind.TransactOpts, id [32]byte, seats []uint16, references [][32]byte, signatures []byte) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "submitReadinessV1", id, seats, references, signatures)
}

// SubmitReadinessV1 is a paid mutator transaction binding the contract method 0xcada9f0d.
//
// Solidity: function submitReadinessV1(bytes32 id, uint16[] seats, bytes32[] references, bytes signatures) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) SubmitReadinessV1(id [32]byte, seats []uint16, references [][32]byte, signatures []byte) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.SubmitReadinessV1(&_FrostWalletRegistry.TransactOpts, id, seats, references, signatures)
}

// SubmitReadinessV1 is a paid mutator transaction binding the contract method 0xcada9f0d.
//
// Solidity: function submitReadinessV1(bytes32 id, uint16[] seats, bytes32[] references, bytes signatures) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) SubmitReadinessV1(id [32]byte, seats []uint16, references [][32]byte, signatures []byte) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.SubmitReadinessV1(&_FrostWalletRegistry.TransactOpts, id, seats, references, signatures)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FrostWalletRegistry *FrostWalletRegistrySession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.TransferOwnership(&_FrostWalletRegistry.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.TransferOwnership(&_FrostWalletRegistry.TransactOpts, newOwner)
}

// WithdrawRewards is a paid mutator transaction binding the contract method 0xc7b8981c.
//
// Solidity: function withdrawRewards() returns(uint96)
func (_FrostWalletRegistry *FrostWalletRegistryTransactor) WithdrawRewards(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FrostWalletRegistry.contract.Transact(opts, "withdrawRewards")
}

// WithdrawRewards is a paid mutator transaction binding the contract method 0xc7b8981c.
//
// Solidity: function withdrawRewards() returns(uint96)
func (_FrostWalletRegistry *FrostWalletRegistrySession) WithdrawRewards() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.WithdrawRewards(&_FrostWalletRegistry.TransactOpts)
}

// WithdrawRewards is a paid mutator transaction binding the contract method 0xc7b8981c.
//
// Solidity: function withdrawRewards() returns(uint96)
func (_FrostWalletRegistry *FrostWalletRegistryTransactorSession) WithdrawRewards() (*types.Transaction, error) {
	return _FrostWalletRegistry.Contract.WithdrawRewards(&_FrostWalletRegistry.TransactOpts)
}

// FrostWalletRegistryDkgExpiredIterator is returned from FilterDkgExpired and is used to iterate over the raw logs and unpacked data for DkgExpired events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryDkgExpiredIterator struct {
	Event *FrostWalletRegistryDkgExpired // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryDkgExpiredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryDkgExpired)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryDkgExpired)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryDkgExpiredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryDkgExpiredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryDkgExpired represents a DkgExpired event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryDkgExpired struct {
	Epoch uint64
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterDkgExpired is a free log retrieval operation binding the contract event 0x60a3d87e581920b0cb996294ea71423f65e9ea7c454087f5b220d7ac5d03798b.
//
// Solidity: event DkgExpired(uint64 indexed epoch)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterDkgExpired(opts *bind.FilterOpts, epoch []uint64) (*FrostWalletRegistryDkgExpiredIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "DkgExpired", epochRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryDkgExpiredIterator{contract: _FrostWalletRegistry.contract, event: "DkgExpired", logs: logs, sub: sub}, nil
}

// WatchDkgExpired is a free log subscription operation binding the contract event 0x60a3d87e581920b0cb996294ea71423f65e9ea7c454087f5b220d7ac5d03798b.
//
// Solidity: event DkgExpired(uint64 indexed epoch)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchDkgExpired(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryDkgExpired, epoch []uint64) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "DkgExpired", epochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryDkgExpired)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "DkgExpired", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDkgExpired is a log parse operation binding the contract event 0x60a3d87e581920b0cb996294ea71423f65e9ea7c454087f5b220d7ac5d03798b.
//
// Solidity: event DkgExpired(uint64 indexed epoch)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseDkgExpired(log types.Log) (*FrostWalletRegistryDkgExpired, error) {
	event := new(FrostWalletRegistryDkgExpired)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "DkgExpired", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryDkgStartedIterator is returned from FilterDkgStarted and is used to iterate over the raw logs and unpacked data for DkgStarted events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryDkgStartedIterator struct {
	Event *FrostWalletRegistryDkgStarted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryDkgStartedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryDkgStarted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryDkgStarted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryDkgStartedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryDkgStartedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryDkgStarted represents a DkgStarted event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryDkgStarted struct {
	Epoch     uint64
	Members   []uint32
	Operators []common.Address
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterDkgStarted is a free log retrieval operation binding the contract event 0x2c7168182f5ca9203996dfcc0bceedb1c0c61a3f3c165ec69473a7af9b16d444.
//
// Solidity: event DkgStarted(uint64 indexed epoch, uint32[] members, address[] operators)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterDkgStarted(opts *bind.FilterOpts, epoch []uint64) (*FrostWalletRegistryDkgStartedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "DkgStarted", epochRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryDkgStartedIterator{contract: _FrostWalletRegistry.contract, event: "DkgStarted", logs: logs, sub: sub}, nil
}

// WatchDkgStarted is a free log subscription operation binding the contract event 0x2c7168182f5ca9203996dfcc0bceedb1c0c61a3f3c165ec69473a7af9b16d444.
//
// Solidity: event DkgStarted(uint64 indexed epoch, uint32[] members, address[] operators)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchDkgStarted(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryDkgStarted, epoch []uint64) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "DkgStarted", epochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryDkgStarted)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "DkgStarted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDkgStarted is a log parse operation binding the contract event 0x2c7168182f5ca9203996dfcc0bceedb1c0c61a3f3c165ec69473a7af9b16d444.
//
// Solidity: event DkgStarted(uint64 indexed epoch, uint32[] members, address[] operators)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseDkgStarted(log types.Log) (*FrostWalletRegistryDkgStarted, error) {
	event := new(FrostWalletRegistryDkgStarted)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "DkgStarted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryOwnershipTransferredIterator struct {
	Event *FrostWalletRegistryOwnershipTransferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryOwnershipTransferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryOwnershipTransferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryOwnershipTransferred represents a OwnershipTransferred event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*FrostWalletRegistryOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryOwnershipTransferredIterator{contract: _FrostWalletRegistry.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryOwnershipTransferred)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseOwnershipTransferred(log types.Log) (*FrostWalletRegistryOwnershipTransferred, error) {
	event := new(FrostWalletRegistryOwnershipTransferred)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryReadinessAcceptedIterator is returned from FilterReadinessAccepted and is used to iterate over the raw logs and unpacked data for ReadinessAccepted events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryReadinessAcceptedIterator struct {
	Event *FrostWalletRegistryReadinessAccepted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryReadinessAcceptedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryReadinessAccepted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryReadinessAccepted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryReadinessAcceptedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryReadinessAcceptedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryReadinessAccepted represents a ReadinessAccepted event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryReadinessAccepted struct {
	WalletId       [32]byte
	DescriptorHash [32]byte
	Seats          []uint16
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterReadinessAccepted is a free log retrieval operation binding the contract event 0x68bbc0cf00d2a6597f922a1160e608d4832cc29fb288d82470a5fd0f8885c0ca.
//
// Solidity: event ReadinessAccepted(bytes32 indexed walletId, bytes32 descriptorHash, uint16[] seats)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterReadinessAccepted(opts *bind.FilterOpts, walletId [][32]byte) (*FrostWalletRegistryReadinessAcceptedIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "ReadinessAccepted", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryReadinessAcceptedIterator{contract: _FrostWalletRegistry.contract, event: "ReadinessAccepted", logs: logs, sub: sub}, nil
}

// WatchReadinessAccepted is a free log subscription operation binding the contract event 0x68bbc0cf00d2a6597f922a1160e608d4832cc29fb288d82470a5fd0f8885c0ca.
//
// Solidity: event ReadinessAccepted(bytes32 indexed walletId, bytes32 descriptorHash, uint16[] seats)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchReadinessAccepted(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryReadinessAccepted, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "ReadinessAccepted", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryReadinessAccepted)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "ReadinessAccepted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseReadinessAccepted is a log parse operation binding the contract event 0x68bbc0cf00d2a6597f922a1160e608d4832cc29fb288d82470a5fd0f8885c0ca.
//
// Solidity: event ReadinessAccepted(bytes32 indexed walletId, bytes32 descriptorHash, uint16[] seats)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseReadinessAccepted(log types.Log) (*FrostWalletRegistryReadinessAccepted, error) {
	event := new(FrostWalletRegistryReadinessAccepted)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "ReadinessAccepted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryRequestsEnabledIterator is returned from FilterRequestsEnabled and is used to iterate over the raw logs and unpacked data for RequestsEnabled events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryRequestsEnabledIterator struct {
	Event *FrostWalletRegistryRequestsEnabled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryRequestsEnabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryRequestsEnabled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryRequestsEnabled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryRequestsEnabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryRequestsEnabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryRequestsEnabled represents a RequestsEnabled event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryRequestsEnabled struct {
	Enabled bool
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRequestsEnabled is a free log retrieval operation binding the contract event 0x829ccc110ba2242259f6b5b2d802c6cc474cb74808a64ab21a57460cee99bc28.
//
// Solidity: event RequestsEnabled(bool enabled)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterRequestsEnabled(opts *bind.FilterOpts) (*FrostWalletRegistryRequestsEnabledIterator, error) {

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "RequestsEnabled")
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryRequestsEnabledIterator{contract: _FrostWalletRegistry.contract, event: "RequestsEnabled", logs: logs, sub: sub}, nil
}

// WatchRequestsEnabled is a free log subscription operation binding the contract event 0x829ccc110ba2242259f6b5b2d802c6cc474cb74808a64ab21a57460cee99bc28.
//
// Solidity: event RequestsEnabled(bool enabled)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchRequestsEnabled(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryRequestsEnabled) (event.Subscription, error) {

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "RequestsEnabled")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryRequestsEnabled)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "RequestsEnabled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRequestsEnabled is a log parse operation binding the contract event 0x829ccc110ba2242259f6b5b2d802c6cc474cb74808a64ab21a57460cee99bc28.
//
// Solidity: event RequestsEnabled(bool enabled)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseRequestsEnabled(log types.Log) (*FrostWalletRegistryRequestsEnabled, error) {
	event := new(FrostWalletRegistryRequestsEnabled)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "RequestsEnabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryResultApprovedIterator is returned from FilterResultApproved and is used to iterate over the raw logs and unpacked data for ResultApproved events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryResultApprovedIterator struct {
	Event *FrostWalletRegistryResultApproved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryResultApprovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryResultApproved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryResultApproved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryResultApprovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryResultApprovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryResultApproved represents a ResultApproved event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryResultApproved struct {
	Epoch          uint64
	ResultHash     [32]byte
	WalletId       [32]byte
	DescriptorHash [32]byte
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterResultApproved is a free log retrieval operation binding the contract event 0x26fbad10f980b75efec6b12d8e4cb35026d4c519f9abe33ad667169b366b0729.
//
// Solidity: event ResultApproved(uint64 indexed epoch, bytes32 indexed resultHash, bytes32 indexed walletId, bytes32 descriptorHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterResultApproved(opts *bind.FilterOpts, epoch []uint64, resultHash [][32]byte, walletId [][32]byte) (*FrostWalletRegistryResultApprovedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var resultHashRule []interface{}
	for _, resultHashItem := range resultHash {
		resultHashRule = append(resultHashRule, resultHashItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "ResultApproved", epochRule, resultHashRule, walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryResultApprovedIterator{contract: _FrostWalletRegistry.contract, event: "ResultApproved", logs: logs, sub: sub}, nil
}

// WatchResultApproved is a free log subscription operation binding the contract event 0x26fbad10f980b75efec6b12d8e4cb35026d4c519f9abe33ad667169b366b0729.
//
// Solidity: event ResultApproved(uint64 indexed epoch, bytes32 indexed resultHash, bytes32 indexed walletId, bytes32 descriptorHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchResultApproved(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryResultApproved, epoch []uint64, resultHash [][32]byte, walletId [][32]byte) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var resultHashRule []interface{}
	for _, resultHashItem := range resultHash {
		resultHashRule = append(resultHashRule, resultHashItem)
	}
	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "ResultApproved", epochRule, resultHashRule, walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryResultApproved)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "ResultApproved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseResultApproved is a log parse operation binding the contract event 0x26fbad10f980b75efec6b12d8e4cb35026d4c519f9abe33ad667169b366b0729.
//
// Solidity: event ResultApproved(uint64 indexed epoch, bytes32 indexed resultHash, bytes32 indexed walletId, bytes32 descriptorHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseResultApproved(log types.Log) (*FrostWalletRegistryResultApproved, error) {
	event := new(FrostWalletRegistryResultApproved)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "ResultApproved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryResultChallengedIterator is returned from FilterResultChallenged and is used to iterate over the raw logs and unpacked data for ResultChallenged events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryResultChallengedIterator struct {
	Event *FrostWalletRegistryResultChallenged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryResultChallengedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryResultChallenged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryResultChallenged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryResultChallengedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryResultChallengedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryResultChallenged represents a ResultChallenged event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryResultChallenged struct {
	Epoch      uint64
	ResultHash [32]byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterResultChallenged is a free log retrieval operation binding the contract event 0xf0668f1933e7ca9482369ea0ce991bb44e521611f75df986153f1606b6b129b7.
//
// Solidity: event ResultChallenged(uint64 indexed epoch, bytes32 indexed resultHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterResultChallenged(opts *bind.FilterOpts, epoch []uint64, resultHash [][32]byte) (*FrostWalletRegistryResultChallengedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var resultHashRule []interface{}
	for _, resultHashItem := range resultHash {
		resultHashRule = append(resultHashRule, resultHashItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "ResultChallenged", epochRule, resultHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryResultChallengedIterator{contract: _FrostWalletRegistry.contract, event: "ResultChallenged", logs: logs, sub: sub}, nil
}

// WatchResultChallenged is a free log subscription operation binding the contract event 0xf0668f1933e7ca9482369ea0ce991bb44e521611f75df986153f1606b6b129b7.
//
// Solidity: event ResultChallenged(uint64 indexed epoch, bytes32 indexed resultHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchResultChallenged(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryResultChallenged, epoch []uint64, resultHash [][32]byte) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var resultHashRule []interface{}
	for _, resultHashItem := range resultHash {
		resultHashRule = append(resultHashRule, resultHashItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "ResultChallenged", epochRule, resultHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryResultChallenged)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "ResultChallenged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseResultChallenged is a log parse operation binding the contract event 0xf0668f1933e7ca9482369ea0ce991bb44e521611f75df986153f1606b6b129b7.
//
// Solidity: event ResultChallenged(uint64 indexed epoch, bytes32 indexed resultHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseResultChallenged(log types.Log) (*FrostWalletRegistryResultChallenged, error) {
	event := new(FrostWalletRegistryResultChallenged)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "ResultChallenged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryResultSubmittedIterator is returned from FilterResultSubmitted and is used to iterate over the raw logs and unpacked data for ResultSubmitted events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryResultSubmittedIterator struct {
	Event *FrostWalletRegistryResultSubmitted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryResultSubmittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryResultSubmitted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryResultSubmitted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryResultSubmittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryResultSubmittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryResultSubmitted represents a ResultSubmitted event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryResultSubmitted struct {
	Epoch      uint64
	ResultHash [32]byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterResultSubmitted is a free log retrieval operation binding the contract event 0x63981ff2f400f06b7afcec41b56f315cf037a8ec4c186c807ddd0806c292beda.
//
// Solidity: event ResultSubmitted(uint64 indexed epoch, bytes32 indexed resultHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterResultSubmitted(opts *bind.FilterOpts, epoch []uint64, resultHash [][32]byte) (*FrostWalletRegistryResultSubmittedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var resultHashRule []interface{}
	for _, resultHashItem := range resultHash {
		resultHashRule = append(resultHashRule, resultHashItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "ResultSubmitted", epochRule, resultHashRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryResultSubmittedIterator{contract: _FrostWalletRegistry.contract, event: "ResultSubmitted", logs: logs, sub: sub}, nil
}

// WatchResultSubmitted is a free log subscription operation binding the contract event 0x63981ff2f400f06b7afcec41b56f315cf037a8ec4c186c807ddd0806c292beda.
//
// Solidity: event ResultSubmitted(uint64 indexed epoch, bytes32 indexed resultHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchResultSubmitted(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryResultSubmitted, epoch []uint64, resultHash [][32]byte) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var resultHashRule []interface{}
	for _, resultHashItem := range resultHash {
		resultHashRule = append(resultHashRule, resultHashItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "ResultSubmitted", epochRule, resultHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryResultSubmitted)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "ResultSubmitted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseResultSubmitted is a log parse operation binding the contract event 0x63981ff2f400f06b7afcec41b56f315cf037a8ec4c186c807ddd0806c292beda.
//
// Solidity: event ResultSubmitted(uint64 indexed epoch, bytes32 indexed resultHash)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseResultSubmitted(log types.Log) (*FrostWalletRegistryResultSubmitted, error) {
	event := new(FrostWalletRegistryResultSubmitted)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "ResultSubmitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FrostWalletRegistryWalletExpiredIterator is returned from FilterWalletExpired and is used to iterate over the raw logs and unpacked data for WalletExpired events raised by the FrostWalletRegistry contract.
type FrostWalletRegistryWalletExpiredIterator struct {
	Event *FrostWalletRegistryWalletExpired // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FrostWalletRegistryWalletExpiredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FrostWalletRegistryWalletExpired)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FrostWalletRegistryWalletExpired)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FrostWalletRegistryWalletExpiredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FrostWalletRegistryWalletExpiredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FrostWalletRegistryWalletExpired represents a WalletExpired event raised by the FrostWalletRegistry contract.
type FrostWalletRegistryWalletExpired struct {
	WalletId [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterWalletExpired is a free log retrieval operation binding the contract event 0xc8a47d32f4f3c7278ceea67de863848fcd8bfc55423405c24bc8f56bd36c79f1.
//
// Solidity: event WalletExpired(bytes32 indexed walletId)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) FilterWalletExpired(opts *bind.FilterOpts, walletId [][32]byte) (*FrostWalletRegistryWalletExpiredIterator, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.FilterLogs(opts, "WalletExpired", walletIdRule)
	if err != nil {
		return nil, err
	}
	return &FrostWalletRegistryWalletExpiredIterator{contract: _FrostWalletRegistry.contract, event: "WalletExpired", logs: logs, sub: sub}, nil
}

// WatchWalletExpired is a free log subscription operation binding the contract event 0xc8a47d32f4f3c7278ceea67de863848fcd8bfc55423405c24bc8f56bd36c79f1.
//
// Solidity: event WalletExpired(bytes32 indexed walletId)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) WatchWalletExpired(opts *bind.WatchOpts, sink chan<- *FrostWalletRegistryWalletExpired, walletId [][32]byte) (event.Subscription, error) {

	var walletIdRule []interface{}
	for _, walletIdItem := range walletId {
		walletIdRule = append(walletIdRule, walletIdItem)
	}

	logs, sub, err := _FrostWalletRegistry.contract.WatchLogs(opts, "WalletExpired", walletIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FrostWalletRegistryWalletExpired)
				if err := _FrostWalletRegistry.contract.UnpackLog(event, "WalletExpired", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseWalletExpired is a log parse operation binding the contract event 0xc8a47d32f4f3c7278ceea67de863848fcd8bfc55423405c24bc8f56bd36c79f1.
//
// Solidity: event WalletExpired(bytes32 indexed walletId)
func (_FrostWalletRegistry *FrostWalletRegistryFilterer) ParseWalletExpired(log types.Log) (*FrostWalletRegistryWalletExpired, error) {
	event := new(FrostWalletRegistryWalletExpired)
	if err := _FrostWalletRegistry.contract.UnpackLog(event, "WalletExpired", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
