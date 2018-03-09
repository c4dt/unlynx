// Package protocolsunlynx contains the adding/removing protocol which permits to change the encryption of data.
// It allows to remove/add a server contribution to the encryption of ciphertexts.
// We assume that the server joining/leaving the cothority participates in the process.
package protocolsunlynx

import (
	"github.com/dedis/kyber"
	"github.com/dedis/onet"
	"github.com/dedis/onet/log"
	"github.com/lca1/unlynx/lib"
)

// AddRmServerProtocolName is the registered name for the local aggregation protocol.
const AddRmServerProtocolName = "AddRmServer"

func init() {
	onet.GlobalProtocolRegister(AddRmServerProtocolName, NewAddRmProtocol)
}

// Protocol
//______________________________________________________________________________________________________________________

// AddRmServerProtocol is a struct holding the state of a protocol instance.
type AddRmServerProtocol struct {
	*onet.TreeNodeInstance

	// Protocol feedback channel
	FeedbackChannel chan []libunlynx.CipherText

	// Protocol state data
	TargetOfTransformation []libunlynx.CipherText
	KeyToRm                kyber.Scalar
	Proofs                 bool
	Add                    bool
}

// NewAddRmProtocol is constructor of add/rm protocol instances.
func NewAddRmProtocol(n *onet.TreeNodeInstance) (onet.ProtocolInstance, error) {
	pvp := &AddRmServerProtocol{
		TreeNodeInstance: n,
		FeedbackChannel:  make(chan []libunlynx.CipherText),
	}

	return pvp, nil
}

var finalResultAddrm = make(chan []libunlynx.CipherText)

// Start is called at the root to start the execution of the Add/Rm protocol.
func (p *AddRmServerProtocol) Start() error {

	log.Lvl1(p.Name(), "starts a server adding/removing Protocol")
	roundComput := libunlynx.StartTimer(p.Name() + "_AddRmServer(PROTOCOL)")
	wg := libunlynx.StartParallelize(len(p.TargetOfTransformation))

	result := make([]libunlynx.CipherText, len(p.TargetOfTransformation))
	result = changeEncryption(p.TargetOfTransformation, p.KeyToRm, p.Add)
	/*
	for i, v := range p.TargetOfTransformation {
		if libunlynx.PARALLELIZE {
			go func(i int, v libunlynx.CipherText) {
				defer wg.Done()
				result[i] = changeEncryptionKeyCipherTexts(v, p.KeyToRm, p.Add)
			}(i, v)
		} else {
			result[i] = changeEncryptionKeyCipherTexts(v, p.KeyToRm, p.Add)
		}
	}

	libunlynx.EndParallelize(wg)
	*/
	libunlynx.EndTimer(roundComput)

	roundProof := libunlynx.StartTimer(p.Name() + "_AddRmServer(PROOFS)")
	pubs := make([]libunlynx.PublishedAddRmProof, 0)
	if p.Proofs {
		proofsCreation(pubs, p.TargetOfTransformation, result, p.KeyToRm, p.Add)
		/*
		wg := libunlynx.StartParallelize(len(result))
		if libunlynx.PARALLELIZE {
		} else {

		}
		for i, v := range result {
			if libunlynx.PARALLELIZE {
				go func(i int, v libunlynx.CipherText) {
					defer wg.Done()
					proofsCreation(pubs, p.TargetOfTransformation[i], v, p.KeyToRm, p.Add)
				}(i, v)

			} else {
				proofsCreation(pubs, p.TargetOfTransformation[i], v, p.KeyToRm, p.Add)
			}

		}
		libunlynx.EndParallelize(wg)
		*/
	}

	libunlynx.EndTimer(roundProof)

	roundProof = libunlynx.StartTimer(p.Name() + "_AddRmServer(PROOFSVerif)")
	wg = libunlynx.StartParallelize(len(pubs))
	for _, v := range pubs {
		if libunlynx.PARALLELIZE {
			go func(v libunlynx.PublishedAddRmProof) {
				defer wg.Done()
				libunlynx.PublishedAddRmCheckProof(v)
			}(v)
		} else {
			libunlynx.PublishedAddRmCheckProof(v)
		}

	}
	libunlynx.EndParallelize(wg)
	libunlynx.EndTimer(roundProof)

	finalResultAddrm <- result
	return nil
}

// Dispatch is called on each node. It waits for incoming messages and handle them.
func (p *AddRmServerProtocol) Dispatch() error {
	aux := <-finalResultAddrm
	p.FeedbackChannel <- aux
	return nil
}

func changeEncryption(cipherTexts []libunlynx.CipherText, serverAddRmKey kyber.Scalar, toAdd bool) []libunlynx.CipherText {
	result := make([]libunlynx.CipherText, len(cipherTexts))

	wg := libunlynx.StartParallelize(len(cipherTexts))
	for i, v := range cipherTexts {
		if libunlynx.PARALLELIZE {
			go func(i int, v libunlynx.CipherText) {
				defer wg.Done()
				result[i] = changeEncryptionKeyCipherTexts(v, serverAddRmKey, toAdd)
			}(i, v)
		} else {
			result[i] = changeEncryptionKeyCipherTexts(v, serverAddRmKey, toAdd)
		}
	}
	libunlynx.EndParallelize(wg)
	return result
}

func changeEncryptionKeyCipherTexts(cipherText libunlynx.CipherText, serverAddRmKey kyber.Scalar, toAdd bool) libunlynx.CipherText {
	tmp := libunlynx.SuiTe.Point().Mul(serverAddRmKey, cipherText.K)
	result := libunlynx.CipherText{}
	result.K = cipherText.K
	if toAdd {
		result.C = libunlynx.SuiTe.Point().Add(cipherText.C, tmp)
	} else {
		result.C = libunlynx.SuiTe.Point().Sub(cipherText.C, tmp)
	}
	return result
}

func proofsCreation(pubs []libunlynx.PublishedAddRmProof, target, ct []libunlynx.CipherText, keyToRm kyber.Scalar, add bool) {
	ktopub := libunlynx.SuiTe.Point().Mul(keyToRm, libunlynx.SuiTe.Point().Base())

	prf := libunlynx.VectorAddRmProofCreation(target, ct, keyToRm, add)
	pub := libunlynx.PublishedAddRmProof{Arp: prf, VectBefore: ct, VectAfter: ct, Krm:ktopub, ToAdd:add}

	pubs = append(pubs, pub)
}
