package serviceDefault

import (
	"github.com/JoaoAndreSa/MedCo/lib"
	"github.com/JoaoAndreSa/MedCo/protocols"
	"github.com/JoaoAndreSa/MedCo/services"
	"github.com/btcsuite/goleveldb/leveldb/errors"
	"github.com/satori/go.uuid"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/crypto.v0/random"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/network"
	"sync"
)

/// ServiceName is the registered name for the medco service.
const ServiceName = "MedCo"

// DROProtocolName is the registered name for the medco service protocol.
const DROProtocolName = "DRO"

const gobFile = "pre_compute_multiplications.gob"
const testDataFile = "medco_test_data.txt"

// SurveyID unique ID for each survey.
type SurveyID string

type SurveyCreationQuery struct {
	SurveyID     SurveyID
	Roster       onet.Roster
	ClientPubKey abstract.Point
	MapDPs       map[string]int64
	Proofs       bool
	AppFlag      bool

	// query statement
	Sum       []string
	Count     bool
	Where     []lib.WhereQueryAttribute
	Predicate string
	GroupBy   []string
}

// Survey represents a survey with the corresponding params
type Survey struct {
	*lib.Store
	Query             SurveyCreationQuery
	SurveySecretKey   abstract.Scalar
	ShufflePrecompute []lib.CipherVectorScalar

	// channels
	SurveyChannel	  chan int  // To wait for the survey to be created before loading data
	DpChannel	  chan int  // To wait for all data to be read before starting medco service protocol
	DDTChannel        chan int  // To wait for all nodes to finish the tagging before continuing
	EndService        chan int  // To wait for the service to end

	Noise         	  lib.CipherText

	Mutex		  sync.Mutex
}

// MsgTypes defines the Message Type ID for all the service's intra-messages.
type MsgTypes struct {
	msgSurveyCreationQuery network.MessageTypeID
	msgSurveyResultsQuery  network.MessageTypeID
	msgDDTfinished         network.MessageTypeID
}

var msgTypes = MsgTypes{}

func init() {
	onet.RegisterNewService(ServiceName, NewService)

	msgTypes.msgSurveyCreationQuery = network.RegisterMessage(&SurveyCreationQuery{})
	network.RegisterMessage(&SurveyResponseQuery{})
	msgTypes.msgSurveyResultsQuery = network.RegisterMessage(&SurveyResultsQuery{})
	msgTypes.msgDDTfinished = network.RegisterMessage(&DDTfinished{})

	network.RegisterMessage(&ServiceState{})
	network.RegisterMessage(&ServiceResult{})
}

// DDTfinished is used to ensure that all servers perform the shuffling+DDT before collectively aggregating the results
type DDTfinished struct{
	SurveyID  SurveyID
}

// SurveyResponseQuery is used to ask a client for its response to a survey.
type SurveyResponseQuery struct {
	SurveyID  SurveyID
	Responses []lib.DpResponseToSend
}

// SurveyResultsQuery is used by querier to ask for the response of the survey.
type SurveyResultsQuery struct {
	IntraMessage bool
	SurveyID     SurveyID
	ClientPublic abstract.Point
}

// ServiceState represents the service "state".
type ServiceState struct {
	SurveyID SurveyID
}

// ServiceResult will contain final results of a survey and be sent to querier.
type ServiceResult struct {
	Results []lib.FilteredResponse
}

// Service defines a service in medco with a survey.
type Service struct {
	*onet.ServiceProcessor

	survey        map[SurveyID]Survey
	/*nbrDPs        int64 // Number of data providers associated with each server
	proofs        bool
	appFlag       bool
	surveyChannel chan int
	dpChannel     chan int
	ddtChannel    chan int
	endService    chan int
	noise         lib.CipherText
	mutex         sync.Mutex*/
}

// NewService constructor which registers the needed messages.
func NewService(c *onet.Context) onet.Service {
	newMedCoInstance := &Service{
		ServiceProcessor: onet.NewServiceProcessor(c),
		survey:           make(map[SurveyID]Survey, 0),
	}
	if cerr := newMedCoInstance.RegisterHandler(newMedCoInstance.HandleSurveyCreationQuery); cerr != nil {
		log.Fatal("Wrong Handler.", cerr)
	}
	if cerr := newMedCoInstance.RegisterHandler(newMedCoInstance.HandleSurveyResponseQuery); cerr != nil {
		log.Fatal("Wrong Handler.", cerr)
	}
	if cerr := newMedCoInstance.RegisterHandler(newMedCoInstance.HandleSurveyResultsQuery); cerr != nil {
		log.Fatal("Wrong Handler.", cerr)
	}
	if cerr := newMedCoInstance.RegisterHandler(newMedCoInstance.HandleDDTfinished); cerr != nil {
		log.Fatal("Wrong Handler.", cerr)
	}

	c.RegisterProcessor(newMedCoInstance, msgTypes.msgSurveyCreationQuery)
	c.RegisterProcessor(newMedCoInstance, msgTypes.msgSurveyResultsQuery)
	c.RegisterProcessor(newMedCoInstance, msgTypes.msgDDTfinished)

	newMedCoInstance.ProtocolRegister(DROProtocolName, newMedCoInstance.NewDROProtocol)
	return newMedCoInstance
}

// Process implements the processor interface and is used to recognize messages broadcasted between servers
func (s *Service) Process(msg *network.Envelope) {
	if msg.MsgType.Equal(msgTypes.msgSurveyCreationQuery) {
		tmp := (msg.Msg).(*SurveyCreationQuery)
		s.HandleSurveyCreationQuery(tmp)
	} else if msg.MsgType.Equal(msgTypes.msgSurveyResultsQuery) {
		tmp := (msg.Msg).(*SurveyResultsQuery)
		s.HandleSurveyResultsQuery(tmp)
	} else if msg.MsgType.Equal(msgTypes.msgDDTfinished) {
		tmp := (msg.Msg).(*DDTfinished)
		s.HandleDDTfinished(tmp)
	}
}

// PushData is used to store incoming data by servers
func (s *Service) PushData(resp *SurveyResponseQuery, proofs bool) {
	for _, v := range resp.Responses {
		dr := lib.DpResponse{}
		dr.FromDpResponseToSend(v)
		(s.survey[resp.SurveyID]).InsertDpResponse(dr, proofs, s.survey[resp.SurveyID].Query.GroupBy, s.survey[resp.SurveyID].Query.Sum, s.survey[resp.SurveyID].Query.Where)
	}
	log.Lvl1(s.ServerIdentity(), " uploaded response data for survey ", resp.SurveyID)
}

// Query Handlers
//______________________________________________________________________________________________________________________

// HandleSurveyCreationQuery handles the reception of a survey creation query by instantiating the corresponding survey.
func (s *Service) HandleSurveyCreationQuery(recq *SurveyCreationQuery) (network.Message, onet.ClientError) {
	log.LLvl1(s.ServerIdentity().String(), " received a Survey Creation Query")

	// if this server is the one receiving the query from the client
	if recq.SurveyID == "" {
		newID := SurveyID(uuid.NewV4().String())
		recq.SurveyID = newID

		log.Lvl1(s.ServerIdentity().String(), " handles this new survey ", recq.SurveyID, " " /*, *recq.SurveyGenID*/)

		// broadcasts the query
		err := services.SendISMOthers(s.ServiceProcessor, &recq.Roster, recq)
		if err != nil {
			log.Error("broadcasting error ", err)
		}
		log.Lvl1(s.ServerIdentity(), " initiated the survey ", newID)

	}

	// chooses an ephemeral secret for this survey
	surveySecret := network.Suite.Scalar().Pick(random.Stream)

	// prepares the precomputation for shuffling
	lineSize := int(len(recq.Sum)) + int(len(recq.Where)) + int(len(recq.GroupBy)) + 1 // + 1 is for the possible count attribute
	precomputeShuffle := services.PrecomputationWritingForShuffling(recq.AppFlag, s.ServerIdentity().String(), gobFile, surveySecret, recq.Roster.Aggregate, lineSize)

	// survey instantiation
	(s.survey[recq.SurveyID]) = Survey{
		Store:             lib.NewStore(),
		Query:             *recq,
		SurveySecretKey:   surveySecret,
		ShufflePrecompute: precomputeShuffle,

		SurveyChannel:    make(chan int, 100),
		DpChannel:        make(chan int, 100),
		DDTChannel:       make(chan int, 100),
		EndService:       make(chan int, 1),
	}
	log.Lvl1(s.ServerIdentity(), " created the survey ", recq.SurveyID)
	log.Lvl1(s.ServerIdentity(), " has a list of ", len(s.survey), " survey(s)")

	if recq.AppFlag {
		//TODO: develop default and i2b2 app
		/*testData := data.ReadDataFromFile(testDataFile)
		resp := EncryptDataToSurvey(s.ServerIdentity().String(), *recq.SurveyID, testData[strconv.Itoa(0)], recq.Roster.Aggregate, 1)
		s.PushData(resp)*/
	}

	// update surveyChannel so that the server knows he can start to process data from DPs
	(s.survey[recq.SurveyID].SurveyChannel) <- 1
	return &ServiceState{recq.SurveyID}, nil
}

// HandleSurveyResponseQuery handles a survey answers submission by a subject.
func (s *Service) HandleSurveyResponseQuery(resp *SurveyResponseQuery) (network.Message, onet.ClientError) {
	if s.survey[resp.SurveyID].Query.SurveyID == resp.SurveyID {
		<-s.survey[resp.SurveyID].SurveyChannel
		s.PushData(resp, s.survey[resp.SurveyID].Query.Proofs)

		//unblock the channel to allow another DP to send its data
		(s.survey[resp.SurveyID].SurveyChannel) <- 1
		//number of data providers who have already pushed the data
		(s.survey[resp.SurveyID].DpChannel) <- 1
		return &ServiceState{"1"}, nil
	}

	log.Lvl1(s.ServerIdentity(), " does not know about this survey!")
	return &ServiceState{resp.SurveyID}, nil
}

// HandleSurveyResultsQuery handles the survey result query by the surveyor.
func (s *Service) HandleSurveyResultsQuery(resq *SurveyResultsQuery) (network.Message, onet.ClientError) {

	log.Lvl1(s.ServerIdentity(), " received a survey result query")
	tmp := s.survey[resq.SurveyID]
	tmp.Query.ClientPubKey = resq.ClientPublic

	// todo why???
	s.survey[resq.SurveyID].Mutex.Lock()
	s.survey[resq.SurveyID] = tmp
	s.survey[resq.SurveyID].Mutex.Unlock()

	if resq.IntraMessage == false {
		resq.IntraMessage = true

		err := services.SendISMOthers(s.ServiceProcessor, &tmp.Query.Roster, resq)
		if err != nil {
			log.Error("broadcasting error ", err)
		}
		s.StartService(resq.SurveyID, true)

		<-s.survey[resq.SurveyID].EndService
		log.Lvl1(s.ServerIdentity(), " completed the query processing...")

		return &ServiceResult{Results: s.survey[resq.SurveyID].PullDeliverableResults()}, nil
	}

	s.StartService(resq.SurveyID, false)
	return nil, nil
}

// HandleDDTfinished handles the message
func (s *Service) HandleDDTfinished(recq *DDTfinished) (network.Message, onet.ClientError) {
	(s.survey[recq.SurveyID].DDTChannel) <- 1
	return nil, nil
}

// Protocol Handlers
//______________________________________________________________________________________________________________________

// NewProtocol creates a protocol instance executed by all nodes
func (s *Service) NewProtocol(tn *onet.TreeNodeInstance, conf *onet.GenericConfig) (onet.ProtocolInstance, error) {
	tn.SetConfig(conf)

	var pi onet.ProtocolInstance
	var err error

	target := SurveyID(string(conf.Data))

	switch tn.ProtocolName() {
	case protocols.ShufflingProtocolName:
		pi, err = protocols.NewShufflingProtocol(tn)
		if err != nil {
			return nil, err
		}
		shuffle := pi.(*protocols.ShufflingProtocol)

		shuffle.Proofs = s.survey[target].Query.Proofs
		shuffle.Precomputed = s.survey[target].ShufflePrecompute
		if tn.IsRoot() {
			dpResponses := s.survey[target].PullDpResponses()
			shuffle.TargetOfShuffle = &dpResponses
		}

	case protocols.DeterministicTaggingProtocolName:
		pi, err = protocols.NewDeterministicTaggingProtocol(tn)
		if err != nil {
			return nil, err
		}
		hashCreation := pi.(*protocols.DeterministicTaggingProtocol)

		aux := s.survey[target].SurveySecretKey
		hashCreation.SurveySecretKey = &aux
		hashCreation.Proofs = s.survey[target].Query.Proofs
		hashCreation.NbrQueryAttributes = len(s.survey[target].Query.Where)
		if tn.IsRoot() {
			shuffledClientResponses := s.survey[target].PullShuffledProcessResponses()
			queryWhereToTag := []lib.ProcessResponse{}
			for _, v := range s.survey[target].Query.Where {
				tmp := lib.CipherVector{v.Value}
				queryWhereToTag = append(queryWhereToTag, lib.ProcessResponse{WhereEnc: tmp, GroupByEnc: nil, AggregatingAttributes: nil})
			}
			shuffledClientResponses = append(queryWhereToTag, shuffledClientResponses...)
			hashCreation.TargetOfSwitch = &shuffledClientResponses

		}

	case protocols.CollectiveAggregationProtocolName:
		pi, err = protocols.NewCollectiveAggregationProtocol(tn)
		if err != nil {
			return nil, err
		}

		groupedData := s.survey[target].PullLocallyAggregatedResponses()
		pi.(*protocols.CollectiveAggregationProtocol).GroupedData = &groupedData
		pi.(*protocols.CollectiveAggregationProtocol).Proofs = s.survey[target].Query.Proofs

		// waits for all other nodes to finish the tagging phase
		counter := len(tn.Roster().List) - 1
		for counter > 0 {
			counter = counter - (<-s.survey[target].DDTChannel)
		}

	case DROProtocolName:

	case protocols.KeySwitchingProtocolName:
		pi, err = protocols.NewKeySwitchingProtocol(tn)
		if err != nil {
			return nil, err
		}

		keySwitch := pi.(*protocols.KeySwitchingProtocol)
		keySwitch.Proofs = s.survey[target].Query.Proofs
		if tn.IsRoot() {
			coaggr := []lib.FilteredResponse{}

			if lib.DIFFPRI == true {
				coaggr = s.survey[target].PullCothorityAggregatedFilteredResponses(true, s.survey[target].Noise)
			} else {
				coaggr = s.survey[target].PullCothorityAggregatedFilteredResponses(false, lib.CipherText{})
			}

			keySwitch.TargetOfSwitch = &coaggr
			tmp1 := s.survey[target].Query.ClientPubKey
			keySwitch.TargetPublicKey = &tmp1
		}
	default:
		return nil, errors.New("Service attempts to start an unknown protocol: " + tn.ProtocolName() + ".")
	}
	return pi, nil
}

// NewDROProtocol implements the DRO protocol - shuffling the noise list
func (s *Service) NewDROProtocol(tn *onet.TreeNodeInstance) (onet.ProtocolInstance, error) {
	pi, err := protocols.NewShufflingProtocol(tn)
	if err != nil {
		return nil, err
	}

	shuffle := pi.(*protocols.ShufflingProtocol)
	shuffle.Proofs = true
	shuffle.Precomputed = nil

	if tn.IsRoot() {
		clientResponses := make([]lib.ProcessResponse, 0)
		noiseArray := lib.GenerateNoiseValues(1000, 0, 1, 0.001)
		for _, v := range noiseArray {
			clientResponses = append(clientResponses, lib.ProcessResponse{GroupByEnc: nil, AggregatingAttributes: *lib.EncryptIntVector(s.survey["lalal"].Query.Roster.Aggregate, []int64{int64(v)})})
		}
		shuffle.TargetOfShuffle = &clientResponses
	}
	return pi, nil
}

// StartProtocol starts a specific protocol (Pipeline, Shuffling, etc.)
func (s *Service) StartProtocol(name string, targetSurvey SurveyID) (onet.ProtocolInstance, error) {
	tmp := s.survey[targetSurvey]
	tree := tmp.Query.Roster.GenerateNaryTreeWithRoot(2, s.ServerIdentity())

	var tn *onet.TreeNodeInstance
	tn = s.NewTreeNodeInstance(tree, tree.Root, name)

	conf := onet.GenericConfig{Data: []byte(string(targetSurvey))}

	pi, err := s.NewProtocol(tn, &conf)

	s.RegisterProtocolInstance(pi)
	go pi.Dispatch()
	go pi.Start()

	return pi, err
}

// Service Phases
//______________________________________________________________________________________________________________________

// StartService starts the service (with all its different steps/protocols)
func (s *Service) StartService(targetSurvey SurveyID, root bool) error {

	log.Lvl1(s.ServerIdentity(), " is waiting on channel")
	<-s.survey[targetSurvey].SurveyChannel

	counter := s.survey[targetSurvey].Query.MapDPs[s.ServerIdentity().String()]
	for counter > int64(0) {
		log.Lvl1(s.ServerIdentity(), " is waiting for ", counter, " data providers to send their data")
		counter = counter - int64(<-s.survey[targetSurvey].DpChannel)
	}
	log.LLvl1("All data providers (", s.survey[targetSurvey].Query.MapDPs[s.ServerIdentity().String()], ") for server ", s.ServerIdentity(), " have sent their data")

	log.LLvl1(s.ServerIdentity(), " starts a Medco Protocol for survey ", targetSurvey)
	target := s.survey[targetSurvey]

	// Shuffling Phase
	start := lib.StartTimer(s.ServerIdentity().String() + "_ShufflingPhase")

	err := s.ShufflingPhase(target.Query.SurveyID)
	if err != nil {
		log.Fatal("Error in the Shuffling Phase")
	}

	lib.EndTimer(start)
	// Tagging Phase
	start = lib.StartTimer(s.ServerIdentity().String() + "_TaggingPhase")

	err = s.TaggingPhase(target.Query.SurveyID)
	if err != nil {
		log.Fatal("Error in the Tagging Phase")
	}

	// broadcasts the query to unlock waiting channel
	aux := s.survey[targetSurvey].Query.Roster
	err = services.SendISMOthers(s.ServiceProcessor, &aux, &DDTfinished{SurveyID: targetSurvey})
	if err != nil {
		log.Error("broadcasting error ", err)
	}

	lib.EndTimer(start)

	// Aggregation Phase
	if root == true {
		start := lib.StartTimer(s.ServerIdentity().String() + "_AggregationPhase")

		err = s.AggregationPhase(target.Query.SurveyID)
		if err != nil {
			log.Fatal("Error in the Aggregation Phase")
		}

		lib.EndTimer(start)
	}

	// DRO Phase
	if root == true && lib.DIFFPRI == true {
		start := lib.StartTimer(s.ServerIdentity().String() + "_DROPhase")

		s.DROPhase(target.Query.SurveyID)

		lib.EndTimer(start)
	}

	// Key Switch Phase
	if root == true {
		start := lib.StartTimer(s.ServerIdentity().String() + "_KeySwitchingPhase")

		s.KeySwitchingPhase(target.Query.SurveyID)

		lib.EndTimer(start)
	}
	(s.survey[targetSurvey].EndService) <- 1

	return nil
}

// ShufflingPhase performs the shuffling of the ClientResponses
func (s *Service) ShufflingPhase(targetSurvey SurveyID) error {
	if len(s.survey[targetSurvey].DpResponses) == 0 && len(s.survey[targetSurvey].DpResponsesAggr) == 0 {
		log.Lvl1(s.ServerIdentity(), " no data to shuffle")
		return nil
	}

	pi, err := s.StartProtocol(protocols.ShufflingProtocolName, targetSurvey)
	if err != nil {
		return err
	}
	shufflingResult := <-pi.(*protocols.ShufflingProtocol).FeedbackChannel

	s.survey[targetSurvey].PushShuffledProcessResponses(shufflingResult)
	return err
}

// TaggingPhase performs the private grouping on the currently collected data.
func (s *Service) TaggingPhase(targetSurvey SurveyID) error {
	if len(s.survey[targetSurvey].ShuffledProcessResponses) == 0 {
		log.LLvl1(s.ServerIdentity(), "  for survey ", s.survey[targetSurvey].Query.SurveyID, " has no data to det tag")
		return nil
	}

	pi, err := s.StartProtocol(protocols.DeterministicTaggingProtocolName, targetSurvey)
	if err != nil {
		return err
	}

	deterministicTaggingResult := <-pi.(*protocols.DeterministicTaggingProtocol).FeedbackChannel

	queryWhereTag := []lib.WhereQueryAttributeTagged{}
	for i, v := range deterministicTaggingResult[:len(s.survey[targetSurvey].Query.Where)] {
		newElem := lib.WhereQueryAttributeTagged{Name: s.survey[targetSurvey].Query.Where[i].Name, Value: v.DetTagWhere[0]}
		queryWhereTag = append(queryWhereTag, newElem)
	}
	deterministicTaggingResult = deterministicTaggingResult[len(s.survey[targetSurvey].Query.Where):]
	filteredResponses := services.FilterResponses(s.survey[targetSurvey].Query.Predicate, queryWhereTag, deterministicTaggingResult)
	s.survey[targetSurvey].PushDeterministicFilteredResponses(filteredResponses, s.ServerIdentity().String(), s.survey[targetSurvey].Query.Proofs)
	return err
}

// AggregationPhase performs the per-group aggregation on the currently grouped data.
func (s *Service) AggregationPhase(targetSurvey SurveyID) error {
	pi, err := s.StartProtocol(protocols.CollectiveAggregationProtocolName, targetSurvey)
	if err != nil {
		return err
	}
	cothorityAggregatedData := <-pi.(*protocols.CollectiveAggregationProtocol).FeedbackChannel
	s.survey[targetSurvey].PushCothorityAggregatedFilteredResponses(cothorityAggregatedData.GroupedData)
	return nil
}

// DROPhase shuffles the list of noise values.
func (s *Service) DROPhase(targetSurvey SurveyID) error {
	tmp := s.survey[targetSurvey]
	tree := tmp.Query.Roster.GenerateNaryTreeWithRoot(2, s.ServerIdentity())

	pi, err := s.CreateProtocol(DROProtocolName, tree)
	if err != nil {
		return err
	}
	go pi.Start()

	shufflingResult := <-pi.(*protocols.ShufflingProtocol).FeedbackChannel

	aux := (s.survey[targetSurvey])
	aux.Noise = shufflingResult[0].AggregatingAttributes[0]

	return nil
}

// KeySwitchingPhase performs the switch to the querier's key on the currently aggregated data.
func (s *Service) KeySwitchingPhase(targetSurvey SurveyID) error {
	pi, err := s.StartProtocol(protocols.KeySwitchingProtocolName, targetSurvey)
	if err != nil {
		return err
	}
	keySwitchedAggregatedResponses := <-pi.(*protocols.KeySwitchingProtocol).FeedbackChannel
	s.survey[targetSurvey].PushQuerierKeyEncryptedResponses(keySwitchedAggregatedResponses)
	return err
}
