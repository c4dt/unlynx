package main

import (
	"errors"
	"github.com/BurntSushi/toml"
	"github.com/lca1/unlynx/lib"
	"github.com/lca1/unlynx/lib/aggregation"
	"github.com/lca1/unlynx/lib/deterministic_tag"
	"github.com/lca1/unlynx/lib/key_switch"
	"github.com/lca1/unlynx/lib/shuffle"
	"github.com/lca1/unlynx/protocols"
	"github.com/lca1/unlynx/protocols/utils"
	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/util/random"
	"go.dedis.ch/onet/v3"
	"go.dedis.ch/onet/v3/log"
)

func init() {
	onet.SimulationRegister("ProofsVerification", NewProofsVerificationSimulation)
}

// ProofsVerificationSimulation holds the state of a simulation.
type ProofsVerificationSimulation struct {
	onet.SimulationBFTree
	NbrServers         int
	NbrDPs             int
	NbrResponses       int
	NbrGroups          int
	NbrGroupAttributes int
	NbrAggrAttributes  int
}

// NewProofsVerificationSimulation constructs a key switching simulation.
func NewProofsVerificationSimulation(config string) (onet.Simulation, error) {
	sim := &ProofsVerificationSimulation{}
	_, err := toml.Decode(config, sim)
	if err != nil {
		return nil, err
	}
	return sim, nil
}

// Setup initializes the simulation.
func (sim *ProofsVerificationSimulation) Setup(dir string, hosts []string) (*onet.SimulationConfig, error) {
	sc := &onet.SimulationConfig{}
	sim.CreateRoster(sc, hosts, 2000)
	err := sim.CreateTree(sc)
	if err != nil {
		return nil, err
	}
	log.Lvl1("Setup done")
	return sc, nil
}

// Run starts the simulation.
func (sim *ProofsVerificationSimulation) Run(config *onet.SimulationConfig) error {

	for round := 0; round < sim.Rounds; round++ {
		log.Lvl1("Starting round", round)
		rooti, err := config.Overlay.CreateProtocol("ProofsVerification", config.Tree, onet.NilServiceID)
		if err != nil {
			return err
		}

		root := rooti.(*protocolsunlynxutils.ProofsVerificationProtocol)
		secKey := libunlynx.SuiTe.Scalar().Pick(random.New())
		pubKey := libunlynx.SuiTe.Point().Mul(secKey, libunlynx.SuiTe.Point().Base())
		secKeyNew := libunlynx.SuiTe.Scalar().Pick(random.New())
		pubKeyNew := libunlynx.SuiTe.Point().Mul(secKeyNew, libunlynx.SuiTe.Point().Base())
		tab := make([]int64, sim.NbrAggrAttributes+sim.NbrGroupAttributes)

		// key switching **********************************************************
		for i := 0; i < len(tab); i++ {
			tab[i] = int64(1)
		}
		cipherVect := *libunlynx.EncryptIntVector(pubKey, tab)

		origEphemKeys := make([]kyber.Point, len(cipherVect))
		origCipherVector := *libunlynx.NewCipherVector(len(cipherVect))
		for i, v := range cipherVect {
			origEphemKeys[i] = v.K
			origCipherVector[i].C = v.C
		}

		_, ks2s, rBNegs, vis := libunlynxkeyswitch.KeySwitchSequence(pubKeyNew, origEphemKeys, secKey)
		keySwitchingProofs := libunlynxkeyswitch.KeySwitchListProofCreation(pubKey, pubKeyNew, secKey, ks2s, rBNegs, vis)

		// deterministic tagging (creation) ****************************************************************************
		tab = make([]int64, sim.NbrGroupAttributes)
		for i := 0; i < len(tab); i++ {
			tab[i] = int64(1)
		}
		cipherVect = *libunlynx.EncryptIntVector(pubKey, tab)

		tagSwitchedVect := libunlynxdetertag.DeterministicTagSequence(cipherVect, secKey, secKeyNew)
		cps := libunlynxdetertag.DeterministicTagCrListProofCreation(cipherVect, tagSwitchedVect, pubKey, secKey, secKeyNew)
		deterministicTaggingCrProofs := cps

		// deterministic tagging (addition) ****************************************************************************
		tab = make([]int64, sim.NbrGroupAttributes)
		for i := 0; i < len(tab); i++ {
			tab[i] = int64(1)
		}
		cipherVect = *libunlynx.EncryptIntVector(pubKey, tab)

		deterministicTaggingAddProofs := libunlynxdetertag.PublishedDDTAdditionListProof{}
		deterministicTaggingAddProofs.List = make([]libunlynxdetertag.PublishedDDTAdditionProof, 0)

		toAdd := libunlynx.SuiTe.Point().Mul(secKeyNew, libunlynx.SuiTe.Point().Base())
		for i := range cipherVect {
			tmp := libunlynx.SuiTe.Point().Add(cipherVect[i].C, toAdd)
			prf, err := libunlynxdetertag.DeterministicTagAdditionProofCreation(cipherVect[i].C, secKeyNew, toAdd, tmp)
			if err != nil {
				return err
			}
			deterministicTaggingAddProofs.List = append(deterministicTaggingAddProofs.List, prf)
		}

		oneVectorProofs := deterministicTaggingAddProofs.List
		for i := 0; i < (sim.NbrResponses*sim.NbrServers)-1; i++ {
			deterministicTaggingAddProofs.List = append(deterministicTaggingAddProofs.List, oneVectorProofs...)

		}

		// local aggregation *******************************************************************************************
		tab = make([]int64, sim.NbrAggrAttributes)
		for i := 0; i < len(tab); i++ {
			tab[i] = int64(1)
		}
		cipherVect = *libunlynx.EncryptIntVector(pubKey, tab)

		tab1 := make([]int64, sim.NbrGroupAttributes)
		for i := 0; i < len(tab1); i++ {
			tab1[i] = int64(1)
		}
		cipherVectGr := *libunlynx.EncryptIntVector(pubKey, tab1)
		testCipherVect1 := *libunlynx.EncryptIntVector(pubKey, tab)

		detResponses := make([]libunlynx.FilteredResponseDet, 0)
		for i := 0; i < sim.NbrGroups; i++ {
			tmp := libunlynx.NewCipherVector(sim.NbrGroupAttributes)
			tmp.Add(cipherVectGr, cipherVectGr)

			cipherVectGr = *tmp
			det1 := cipherVectGr
			protocolsunlynx.TaggingDet(&det1, secKey, secKey, pubKey, false)
			deterministicGroupAttributes := make(libunlynx.DeterministCipherVector, len(det1))

			for j, c := range det1 {
				deterministicGroupAttributes[j] = libunlynx.DeterministCipherText{Point: c.C}
			}

			newDetResponse := libunlynx.FilteredResponseDet{Fr: libunlynx.FilteredResponse{GroupByEnc: cipherVectGr, AggregatingAttributes: testCipherVect1}, DetTagGroupBy: deterministicGroupAttributes.Key()}
			for j := 0; j < (sim.NbrResponses/sim.NbrServers)/sim.NbrGroups; j++ {
				detResponses = append(detResponses, newDetResponse)
			}
		}

		comparisonMap := make(map[libunlynx.GroupingKey]libunlynx.FilteredResponse)
		cvMap := make(map[libunlynx.GroupingKey][]libunlynx.CipherVector)
		for _, v := range detResponses {
			libunlynx.AddInMap(comparisonMap, v.DetTagGroupBy, v.Fr)
			v.FormatAggregationProofs(cvMap)
		}

		aggregationProofs := libunlynxaggr.PublishedAggregationListProof{}
		for k, v := range cvMap {
			aggregationProofs = libunlynxaggr.AggregationListProofCreation(v, comparisonMap[k].AggregatingAttributes)

			for i := 0; i <= sim.NbrServers; i++ {
				aggregationProofs.List = append(aggregationProofs.List, aggregationProofs.List...)
			}
		}

		//shuffling ****************************************************************************************************
		log.Lvl1("Starting shuffling (can take some time)")
		responsesToShuffle := make([]libunlynx.ProcessResponse, sim.NbrResponses/sim.NbrServers)
		for i := 0; i < sim.NbrResponses/sim.NbrServers; i++ {
			responsesToShuffle[i] = libunlynx.ProcessResponse{GroupByEnc: cipherVectGr, AggregatingAttributes: testCipherVect1}
		}

		listCV, _ := protocolsunlynx.ProcessResponseToMatrixCipherText(responsesToShuffle)
		clientResponsesShuffled, pi, beta := libunlynxshuffle.ShuffleSequence(listCV, libunlynx.SuiTe.Point().Base(), root.Roster().Aggregate, nil)
		log.Lvl1("Starting shuffling proof creation")
		shufflingProof := libunlynxshuffle.ShuffleProofCreation(listCV, clientResponsesShuffled, libunlynx.SuiTe.Point().Base(), root.Roster().Aggregate, beta, pi)

		shufflingProofs := libunlynxshuffle.PublishedShufflingListProof{}
		shufflingProofs.List = make([]libunlynxshuffle.PublishedShufflingProof, sim.NbrServers*sim.NbrServers)
		for i := range shufflingProofs.List {
			shufflingProofs.List[i] = shufflingProof
		}

		//collective aggregation ***************************************************************************************
		cvMap = make(map[libunlynx.GroupingKey][]libunlynx.CipherVector)
		c1 := make(map[libunlynx.GroupingKey]libunlynx.FilteredResponse)
		for _, v := range detResponses {
			libunlynx.AddInMap(c1, v.DetTagGroupBy, v.Fr)
		}

		c3 := make(map[libunlynx.GroupingKey]libunlynx.FilteredResponse)
		for i, v := range c1 {
			libunlynx.AddInMap(c3, i, v)
			libunlynx.AddInMap(c3, i, v)
			frd := libunlynx.FilteredResponseDet{DetTagGroupBy: i, Fr: v}
			frd.FormatAggregationProofs(cvMap)
			frd.FormatAggregationProofs(cvMap)
		}

		collAggrProofs := libunlynxaggr.PublishedAggregationListProof{}
		collAggrProofs.List = make([]libunlynxaggr.PublishedAggregationProof, 0)
		for k, v := range cvMap {
			collAggrProofs.List = append(collAggrProofs.List, libunlynxaggr.AggregationListProofCreation(v, c3[k].AggregatingAttributes).List...)
		}
		root.ProtocolInstance().(*protocolsunlynxutils.ProofsVerificationProtocol).TargetOfVerification = protocolsunlynxutils.ProofsToVerify{KeySwitchingProofs: keySwitchingProofs,
			DetTagCreationProofs: deterministicTaggingCrProofs, DetTagAdditionProofs: deterministicTaggingAddProofs, AggregationProofs: aggregationProofs, ShufflingProofs: shufflingProofs, CollectiveAggregationProofs: collAggrProofs}

		round := libunlynx.StartTimer("ProofsVerification(SIMULATION)")

		if err := root.Start(); err != nil {
			return err
		}
		results := <-root.ProtocolInstance().(*protocolsunlynxutils.ProofsVerificationProtocol).FeedbackChannel
		libunlynx.EndTimer(round)

		log.Lvl1(len(results), " proofs verified")

		if results[0] == false {
			return errors.New("key switching proofs failed")
		} else if results[1] == false {
			return errors.New("deterministic tagging (creation) proofs failed")
		} else if results[2] == false {
			return errors.New("deterministic tagging (addition) proofs failed")
		} else if results[3] == false {
			return errors.New("local aggregation proofs failed")
		} else if results[4] == false {
			return errors.New("shuffling proofs failed")
		} else if results[5] == false {
			return errors.New("collective aggregation proofs failed")
		}
	}
	return nil
}
