package testsync_chaincode


import (
	"errors"
	"fmt"
	"github.com/abchain/fabric/core/chaincode/shim"
	"strconv"
	"time"
)

// TestSyncChaincode example simple Chaincode implementation
type TestSyncChaincode struct {
}

var logger = shim.NewLogger("ecc")

func (t *TestSyncChaincode) Init(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {

	var A, B string    // Entities
	var Aval, Bval int // Asset holdings
	var err error
	var b []byte

	b, err = stub.GetCallerCertificate()
	if err != nil {
		logger.Warning("Security fail", err)
	} else if b == nil {
		logger.Info("No security")
	} else {
		logger.Info("Get security", len(b))
	}

	if len(args) != 4 {
		return nil, errors.New("Incorrect number of arguments. Expecting 4")
	}

	// Initialize the chaincode
	A = args[0]
	Aval, err = strconv.Atoi(args[1])
	if err != nil {
		return nil, errors.New("Expecting integer value for asset holding")
	}
	B = args[2]
	Bval, err = strconv.Atoi(args[3])
	if err != nil {
		return nil, errors.New("Expecting integer value for asset holding")
	}
	fmt.Printf("Aval = %d, Bval = %d\n", Aval, Bval)

	t.batchPutState(stub, A, 1, Aval)
	t.batchPutState(stub, B, 1, Bval)

	return nil, nil
}


func (t *TestSyncChaincode) batchPutState(stub shim.ChaincodeStubInterface, key string, startValue int, endValue int) ([]byte, error) {

	t.putInt(stub, key, endValue)
	for i := startValue; i <= endValue; i++ {

		if i % 1000 == 0{
			<-time.After(2 * time.Second)
		}

		k := fmt.Sprintf("%s%d", key, i)
		t.putInt(stub, k, i)
	}

	return nil, nil
}

// Transaction makes payment of X units from A to B
func (t *TestSyncChaincode) Invoke(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	if function == "delete" {
		// Deletes an entity from its state
		return t.delete(stub, args)
	}

	attr, errattr := stub.ReadCertAttribute("position")
	if errattr != nil {
		fmt.Println("attr fail", errattr)
	} else {
		fmt.Println("attr position is", string(attr))
	}

	var A, B string    // Entities
	var Aval, Bval int // Asset holdings
	var X int          // Transaction value
	var err error

	if len(args) != 3 {
		return nil, errors.New("Incorrect number of arguments. Expecting 3")
	}

	A = args[0]
	B = args[1]

	// Get the state from the ledger
	// TODO: will be nice to have a GetAllState call to ledger
	Avalbytes, err := stub.GetState(A)
	if err != nil {
		return nil, errors.New("Failed to get state")
	}
	if Avalbytes == nil {
		return nil, errors.New("Entity not found")
	}
	Aval, _ = strconv.Atoi(string(Avalbytes))

	Bvalbytes, err := stub.GetState(B)
	if err != nil {
		return nil, errors.New("Failed to get state")
	}
	if Bvalbytes == nil {
		return nil, errors.New("Entity not found")
	}
	Bval, _ = strconv.Atoi(string(Bvalbytes))

	// Perform the execution
	X, err = strconv.Atoi(args[2])
	if err != nil {
		return nil, errors.New("Invalid transaction amount, expecting a integer value")
	}
	Aval = Aval - X
	Bval = Bval + X
	fmt.Printf("Aval = %d, Bval = %d\n", Aval, Bval)

	keysize, _ := t.getInt(stub,"keysize")

	t.batchPutState(stub, "breakpoint_", keysize + 1, keysize + X)
	keysize += X
	t.putInt(stub, "keysize", keysize)
	fmt.Printf("keysize = %d\n", keysize)

	// Write the state back to the ledger
	err = stub.PutState(A, []byte(strconv.Itoa(Aval)))
	if err != nil {
		return nil, err
	}

	err = stub.PutState(B, []byte(strconv.Itoa(Bval)))
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func (t *TestSyncChaincode) getInt(stub shim.ChaincodeStubInterface, key string) (int, error) {

	Avalbytes, err := stub.GetState(key)
	if err != nil {
		return 0, errors.New("Failed to get state")
	}
	if Avalbytes == nil {
		return 0, errors.New("Entity not found")
	}

	return strconv.Atoi(string(Avalbytes))
}


func (t *TestSyncChaincode) putInt(stub shim.ChaincodeStubInterface, key string, val int) {
	err := stub.PutState(key, []byte(strconv.Itoa(val)))

	if err != nil {
	}
}


// Deletes an entity from state
func (t *TestSyncChaincode) delete(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	if len(args) != 1 {
		return nil, errors.New("Incorrect number of arguments. Expecting 1")
	}

	A := args[0]

	// Delete the key from the state in ledger
	err := stub.DelState(A)
	if err != nil {
		return nil, errors.New("Failed to delete state")
	}

	return nil, nil
}

// Query callback representing the query of a chaincode
func (t *TestSyncChaincode) Query(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	if function != "query" {
		return nil, errors.New("Invalid query function name. Expecting \"query\"")
	}
	var A string // Entities
	var err error

	attr, errattr := stub.ReadCertAttribute("position")
	if errattr != nil {
		fmt.Println("attr fail", errattr)
	} else {
		fmt.Println("attr position is", string(attr))
	}

	if len(args) != 1 {
		return nil, errors.New("Incorrect number of arguments. Expecting name of the person to query")
	}

	A = args[0]

	// Get the state from the ledger
	Avalbytes, err := stub.GetState(A)
	if err != nil {
		jsonResp := "{\"Error\":\"Failed to get state for " + A + "\"}"
		return nil, errors.New(jsonResp)
	}

	if Avalbytes == nil {
		jsonResp := "{\"Error\":\"Nil amount for " + A + "\"}"
		return nil, errors.New(jsonResp)
	}

	jsonResp := "{\"Name\":\"" + A + "\",\"Amount\":\"" + string(Avalbytes) + "\"}"
	fmt.Printf("Query Response:%s\n", jsonResp)
	return Avalbytes, nil
}

