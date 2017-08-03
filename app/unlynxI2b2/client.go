package main

// I2b2 Unlynx client

import (
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/app"
	"gopkg.in/dedis/onet.v1/log"
	"github.com/lca1/unlynx/lib"
	"os"
	"strings"
	"strconv"
	"regexp"
	"gopkg.in/urfave/cli.v1"
	"encoding/xml"
	"io/ioutil"
	"gopkg.in/dedis/crypto.v0/abstract"
	"io"
	"github.com/lca1/unlynx/services/i2b2"
	"time"
)

// Client functions
//______________________________________________________________________________________________________________________

// read from a reader an xml (until EOF), and unmarshal it
func readQueryXMLFrom(input io.Reader) (*lib.XMLMedCoQuery, error) {

	// read from stdin TODO: limit the amount read
	dataBytes, err_io := ioutil.ReadAll(input)

	if err_io != nil {
		log.Error("Error while reading standard input.", err_io)
		return nil, err_io
	} else {
		log.Info("Correctly read standard input until EOF.")
	}

	// unmarshal xml (assumes bytes are UTF-8 encoded)
	parsed_xml := lib.XMLMedCoQuery{}
	err_xml := xml.Unmarshal(dataBytes, &parsed_xml)
	if err_xml != nil {
		log.Error("Error while unmarshalling xml.", err_xml)
		return nil, err_xml
	}

	return &parsed_xml, nil
}

func unlynxQueryFromApp(c *cli.Context) error {

	// cli arguments
	groupFilePath := c.String("file")
	// TODO: use the serverIdentityID / UUID + el.Search rather than the entry point index
	entryPointIdx := c.Int("entryPointIdx")
	proofs := c.Bool("proofs")

	// generate el with group file
	f, err := os.Open(groupFilePath)
	if err != nil {
		log.Error("Error while opening group file", err)
		return cli.NewExitError(err, 1)
	}
	el, err := app.ReadGroupToml(f)
	if err != nil {
		log.Error("Error while reading group file", err)
		return cli.NewExitError(err, 1)
	}
	if len(el.List) <= 0 {
		log.Error("Empty or invalid group file", err)
		return cli.NewExitError(err, 1)
	}

	err = unlynxQuery(os.Stdin, os.Stdout, el, entryPointIdx, proofs)
	if err != nil {
		log.Error("Error while querying Unlynx", err)
		return cli.NewExitError(err, 2)
	}

	return nil
}

// TODO: no log.Fatal in general (this stops immediately)
// TODO: handle errors in to/from bytes in crypto.go
// run unlynx query, all errors will be sent to the output
func unlynxQuery(input io.Reader, output io.Writer, el *onet.Roster, entryPointIdx int, proofs bool) error {
	startT := time.Now();

	// get data from input
	xmlQuery, err := readQueryXMLFrom(input);
	if err != nil {
		log.Error("Error parsing XML.", err)
		writeResultXML(output, nil, -1, nil, serviceI2B2.TimeResults{}, err)
		return err
	}

	// parse query
	encWhereValuesParsed, predicateParsed, resultModeParsed, clientPublicKey, err := parseQuery(xmlQuery)
	if err != nil {
		log.Error("Error parsing query fields.", err)
		writeResultXML(output, xmlQuery, -1, nil, serviceI2B2.TimeResults{}, err)
		return err
	}

	// setup number of data providers TODO: hardcoded to 1 DP / server
	nbrDPs := make(map[string]int64)
	for _, server := range el.List {
		nbrDPs[server.String()] = 1
	}

	// get patients data
	patientsData, err := xmlQuery.PatientsDataToUnlynxFormat(el)
	if err != nil {
		log.Error("Error extracing patients data.", err)
		writeResultXML(output, xmlQuery, -1, nil, serviceI2B2.TimeResults{}, err)
		return err
	}
	parsingTime := time.Since(startT)

	// launch query
	start := time.Now();

	// remove data and only put one entry
	//patientsData = patientsData[:1]
	//patientsData[0].WhereEnc = patientsData[0].WhereEnc[:1]
	client := serviceI2B2.NewUnLynxClient(el.List[entryPointIdx], strconv.Itoa(entryPointIdx))
	_, result, tr, err := client.SendSurveyDpQuery(
		el, // entities
		serviceI2B2.SurveyID(xmlQuery.QueryId), // surveyGenId
		serviceI2B2.SurveyID(""), // surveyID
		clientPublicKey, // clientPubKey
		nbrDPs, // number of DPs per server
		proofs, // compute proofs
		false, // appFlag: data is passed with query (not via separate file)
		[]string{"s1"}, // aggregating attribute TODO: to be changed to support dummies
		false, // count flag
		encWhereValuesParsed, // encrypted where query
		predicateParsed, // predicate
		[]string{}, // groupBy
		patientsData, // encrypted patients data
		int64(resultModeParsed), // mode: 0 (each DP different result) or 1 (everyone same aggregation)
		start,
	)
	totalTime := time.Since(start)

	if err != nil {
		log.Error("Error during query.", err)
		writeResultXML(output, xmlQuery, -1, nil, serviceI2B2.TimeResults{}, err)
		return err
	}

	// sanity check
	if len(result.AggregatingAttributes) != 1 {
		log.Warn("Length of result is >1, error is possible (" + strconv.Itoa(len(result.AggregatingAttributes)) + ")")
	}

	tr.CommunTime = totalTime-tr.ExecTime
	tr.ParsingTime = parsingTime
	tr.ExecTime += tr.ParsingTime
	err = writeResultXML(output, xmlQuery, resultModeParsed, &result.AggregatingAttributes[0], tr,  nil)
	if err != nil {
		log.Error("Error while writing result.", err)
		return err
	}
	return nil
}

// output result xml on a writer (if result_err != nil, the error is sent)
func writeResultXML(output io.Writer, xmlQuery *lib.XMLMedCoQuery, resultModeParsed int,
	ctResult *lib.CipherText, tr serviceI2B2.TimeResults, result_err error) error {

	log.LLvl1("\n\n" +
		  "#########---- TIME ----#########" + "\n" +
		  "Total execution time: " + tr.ExecTime.String() + "\n" +
		  "Total communication time: " + tr.CommunTime.String() + "\n" +
		  "Total parsing time (i2b2 -> unlynx client): " + tr.ParsingTime.String() + "\n" +
		  "Total broadcast time (unlynx client -> unlynx server): " + tr.SendingTime.String() + "\n" +
		  "DDT Query execution time: " + tr.DDTQueryTimeExec.String() + "\n" +
		  "DDT Query communication time: " + tr.DDTQueryTimeCommun.String() + "\n" +
		  "DDT Data execution time: " + tr.DDTDataTimeExec.String() + "\n" +
	          "DDT Data communication time: " + tr.DDTDataTimeCommun.String() + "\n" +
		  "Aggregation time: " + tr.AggrTimeExec.String() + "\n" +
		  "Shuffling execution time: " + tr.ShuffTimeExec.String() + "\n" +
		  "Shuffling communication time: " + tr.ShuffCommunExec.String() + "\n" +
		  "Key Switching execution time: " + tr.KeySTimeExec.String() + "\n" +
		  "Key Switching communication time: " + tr.KeySTimeCommun.String() + "\n")

	resultString := ""
	if result_err == nil {
		resultString = `<medco_query_result>
	<id>` + (*xmlQuery).QueryId + `</id>
	<result_mode>` + strconv.Itoa(resultModeParsed) + `</result_mode>
	<enc_result>` + (*ctResult).Serialize() + `</enc_result>
	<times_ms>{"Unlynx execution time":` + strconv.FormatInt(int64(tr.ExecTime.Nanoseconds()/1000000.0), 10) +
			`,"Unlynx communication time":` + strconv.FormatInt(int64(tr.CommunTime.Nanoseconds()/1000000.0), 10) +
			`,"Parsing time":` + strconv.FormatInt(int64(tr.ParsingTime.Nanoseconds()/1000000.0), 10) +
			`,"Broadcasting time":` + strconv.FormatInt(int64(tr.SendingTime.Nanoseconds()/1000000.0), 10) +
			`,"DDT Query execution time":` + strconv.FormatInt(int64(tr.DDTQueryTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"DDT Query communication time":` + strconv.FormatInt(int64(tr.DDTQueryTimeCommun.Nanoseconds()/1000000.0), 10) +
			`,"DDT Data execution time":` + strconv.FormatInt(int64(tr.DDTDataTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"DDT Data communication time":` + strconv.FormatInt(int64(tr.DDTDataTimeCommun.Nanoseconds()/1000000.0), 10) +
			`,"Aggregation time":` + strconv.FormatInt(int64(tr.AggrTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"Shuffling execution time":` + strconv.FormatInt(int64(tr.ShuffTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"Shuffling communication time":` + strconv.FormatInt(int64(tr.ShuffCommunExec.Nanoseconds()/1000000.0), 10) +
			`,"Key Switching execution time":` + strconv.FormatInt(int64(tr.KeySTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"Key Switching communication time":` + strconv.FormatInt(int64(tr.KeySTimeCommun.Nanoseconds()/1000000.0), 10) +
			`}</times_ms>
</medco_query_result>
`
	} else if xmlQuery != nil {
		resultString = `<medco_query_result>
	<id>` + (*xmlQuery).QueryId + `</id>
	<error>` + result_err.Error() + `</error>
</medco_query_result>
`
	} else {
		resultString = `<medco_query_result>
	<id>unknown</id>
	<error>` + result_err.Error() + `</error>
</medco_query_result>
`
	}

	_, err := io.WriteString(output, resultString)
	if err != nil {
		log.Error("Error while writing result.", err)
		return err
	}
	return nil
}

func checkRegex(input, expression, errorMessage string) {
	var aux = regexp.MustCompile(expression)

	correct := aux.MatchString(input)

	if !correct {
		log.Error(errorMessage)
	}
}

// parse arguments in proper unlynx query format and check their correctness
func parseQuery(xmlQuery *lib.XMLMedCoQuery) ([]lib.WhereQueryAttribute, string, int, abstract.Point, error) {

	// sanity checks
	where := (*xmlQuery).EncWhereValues
	predicate := (*xmlQuery).Predicate
	resultMode, errResultMode := strconv.Atoi((*xmlQuery).ResultMode)

	if (where != "" && predicate == "") || (where == "" && predicate != "") ||
		errResultMode != nil || resultMode < 0 || resultMode > 1 {
		log.Error("Wrong query! Please check the resultMode, where and predicate parameters.")
	}

	// check where formatting
	b64StdAlphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	whereRegex := "{(w[0-9]+(,\\s*[" + b64StdAlphabet + "]+))*(,\\s*w[0-9]+(,\\s*[" + b64StdAlphabet + "]+))*}"

	checkRegex(where, whereRegex, "Error parsing the where parameter(s)")
	where = strings.Replace(where, " ", "", -1)
	where = strings.Replace(where, "{", "", -1)
	where = strings.Replace(where, "}", "", -1)
	tmp := strings.Split(where, ",")

	whereFinal := make([]lib.WhereQueryAttribute, 0)

	var variable string
	for i := range tmp {
		// if is a variable (w1, w2...)
		if i%2 == 0 {
			variable = tmp[i]
		} else { // if it is a value
			whereFinal = append(whereFinal, lib.WhereQueryAttribute{Name: variable, Value: *lib.NewCipherTextFromBase64(tmp[i])})
		}
	}

	// deserialize client public key
	clientPubKey, err := lib.DeserializePoint((*xmlQuery).ClientPublicKeyB64)
	if err != nil {
		log.Error("Error while deserializing the client public key.", err)
		return nil, "", 0, nil, err
	}

	// TODO: predicate correctness done by the external library at a later stage (should be checked here)
	return whereFinal, predicate, resultMode, clientPubKey, nil
}