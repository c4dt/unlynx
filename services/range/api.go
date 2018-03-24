package servicesunlynxrange

/*import (
	"github.com/dedis/paper_17_dfinity/pbc"
	"github.com/lca1/unlynx/lib"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/network"
)

//Structs_______________________________________________________________________________________________

//StructProofRangeByte is structure to send in bytes.
type StructProofRangeByte struct {
	Roster     *onet.Roster
	RequestID  string
	EntryPoint bool
	Commit     libUnLynx.CipherText
	Challenge  abstract.Scalar
	Zr         abstract.Scalar
	D          abstract.Point
	Zv         []abstract.Scalar
	Zphi       []abstract.Scalar
	V          [][]byte
	A          [][]byte
}

//VerifResult used to check result at the end.
type VerifResult struct {
	Res int64
}

//API represents the data provider
type API struct {
	*onet.Client
	ClientID    string
	secretValue []int64
	ToPublish   []libUnLynx.PublishRangeProof
	CAPublic    abstract.Point
	EntryPoint  *network.ServerIdentity
}

//NewUnlynxRangeClient creates a new Client with given values.
func NewUnlynxRangeClient(clientID string) *API {

	newClient := &API{
		Client:      onet.NewClient(ServiceName),
		ClientID:    clientID,
		secretValue: []int64{63, 16, 154816, 1, 18946, 48918896418965},
	}
	return newClient
}

//SendRequest sends a request to get signature from a server. Also compute the proof from the signatures.
func (c *API) SendRequest(entities *onet.Roster, key abstract.Point) (string, error) {
	servList := entities.List
	//structure response and pairing used
	sig := ServiceSig{}
	pairing := pbc.NewPairingFp254BNb()
	c.ToPublish = make([]libUnLynx.PublishRangeProof, len(servList))

	for i := 0; i < len(servList); i++ {
		//reset structure
		sig = ServiceSig{}

		//send server that you need the signature
		c.SendProtobuf(servList[i], &DataDP{ClientPublic: key, CAPublic: c.CAPublic, RequestID: []byte(c.ClientID)}, &sig)

		//here need to get back signature that were transferred in bytes
		signatureStruct := libUnLynx.PublishSignature{Public: sig.Signature.Public, Signature: make([]abstract.Point, len(sig.Signature.Signature))}
		for j := 0; j < len(sig.Signature.Signature); j++ {
			//point we are going to deserialize
			point := pairing.G1().Point()

			err := point.UnmarshalBinary(sig.Signature.Signature[j])
			if err != nil {
				log.Fatal("Cannot deserialize ", err)
			}
			signatureStruct.Signature[j] = point
		}
		//For each server, and each secret you have , compute the predicate for proving secret_k
		//is in the range given. Each server will verify it.
		c.ToPublish[i] = libUnLynx.CreatePredicateRangeProof(signatureStruct, sig.U, sig.L, c.secretValue[0], c.CAPublic)

	}

	//return the id of the request in the concurrent map of service if successful
	return string(sig.RequestID), nil
}

//ExecuteProof sends proof to server. Return aggreg if service was launched. 0 if all verify succesfully.
func (c *API) ExecuteProof(entities *onet.Roster, id string) (int64, error) {
	servList := entities.List
	verifResultFin := VerifResult{0}
	//	start := time.Now()
	for i, v := range servList {
		verifResult := VerifResult{0}
		//the data you'll send for proof verification
		datas := c.ToPublish[i]

		//the orginal entry for the data is only 1 server, the other will discard the datas.
		entry := false
		if v.Equal(c.EntryPoint) {
			entry = true
		}

		rangeProofs := StructProofRangeByte{Roster: entities, RequestID: id, EntryPoint: entry, Commit: datas.Cipher, Challenge: datas.Challenge, Zr: datas.Zr, D: datas.D, Zv: datas.Zv, Zphi: datas.Zphi}
		//get to []abstract.Point to [][]byte
		Vbyte, Abyte := make([][]byte, len(datas.V)), make([][]byte, len(datas.A))
		for j := 0; j < len(datas.A); j++ {
			dataA, err := datas.A[j].MarshalBinary()
			if err != nil {
				log.Fatal("Error in serialization")
			}
			dataV, err := datas.V[j].MarshalBinary()
			if err != nil {
				log.Fatal("Error in serialization")
			}
			Vbyte[j] = dataV
			Abyte[j] = dataA
		}
		rangeProofs.V, rangeProofs.A = Vbyte, Abyte
		//Send to each server for verification, return a boolean for each
		err := c.SendProtobuf(v, &rangeProofs, &verifResult)

		if err != nil {
			return 1, err
		}
		verifResultFin.Res += verifResult.Res
	}
*/
/*time := time.Since(start)
filename := "/home/unlynx/go/src/unlynx/services/timeVerif"
f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0600)
if err != nil {
	panic(err)
}
defer f.Close()
if _, err = f.WriteString(time.String() + "\n"); err != nil {
	panic(err)
}
*/ /*

	return verifResultFin.Res, nil
}*/