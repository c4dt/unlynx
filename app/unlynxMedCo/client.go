package main

// MedCo Unlynx client

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"github.com/lca1/unlynx/app/unlynxMedCo/loader"
	"github.com/lca1/unlynx/lib"
	"github.com/lca1/unlynx/services/unlynxMedCo"
	_ "github.com/lib/pq"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/app"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/urfave/cli.v1"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"time"
)

// Loader functions
//______________________________________________________________________________________________________________________

//----------------------------------------------------------------------------------------------------------------------
//#----------------------------------------------- LOAD DATA -----------------------------------------------------------
//----------------------------------------------------------------------------------------------------------------------

func loadData(c *cli.Context) error {

	// data set file paths
	clinicalOntologyPath := c.String("ont_clinical")
	genomicOntologyPath := c.String("ont_genomic")
	clinicalFilePath := c.String("clinical")
	genomicFilePath := c.String("genomic")
	groupFilePath := c.String("file")
	entryPointIdx := c.Int("entryPointIdx")
	listSensitive := c.StringSlice("sensitive")
	replaySize := c.Int("replay")

	// db settings
	dbHost := c.String("dbHost")
	dbPort := c.Int("dbPort")
	dbName := c.String("dbName")
	dbUser := c.String("dbUser")
	dbPassword := c.String("dbPassword")

	databaseS := loader.DBSettings{DBhost: dbHost, DBport: dbPort, DBname: dbName, DBuser: dbUser, DBpassword: dbPassword}

	// check if db connection works
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+"password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPassword, dbName)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		log.Error("Error while opening database", err)
		return cli.NewExitError(err, 1)
	}
	db.Close()

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

	fOntClinical, err := os.Open(clinicalOntologyPath)
	if err != nil {
		log.Error("Error while opening the clinical ontology file", err)
		return cli.NewExitError(err, 1)
	}

	fOntGenomic, err := os.Open(genomicOntologyPath)
	if err != nil {
		log.Error("Error while opening the genomic ontology file", err)
		return cli.NewExitError(err, 1)
	}

	fClinical, err := os.Open(clinicalFilePath)
	if err != nil {
		log.Error("Error while opening the clinical file", err)
		return cli.NewExitError(err, 1)
	}

	fGenomic, err := os.Open(genomicFilePath)
	if err != nil {
		log.Error("Error while opening the genomic file", err)
		return cli.NewExitError(err, 1)
	}

	if listSensitive == nil {
		log.Error("Error while parsing list of sensitive files", err)
		return cli.NewExitError(err, 1)
	}

	if replaySize < 1 {
		log.Error("Wrong file size value (1>)", err)
		return cli.NewExitError(err, 1)
	} else if replaySize > 1 {
		fGenomic.Close()
		loader.ReplayDataset(genomicFilePath, replaySize)

		fGenomic, err = os.Open(genomicFilePath)
		if err != nil {
			log.Error("Error while opening the new genomic file", err)
			return cli.NewExitError(err, 1)
		}
	}

	loader.LoadClient(el, entryPointIdx, fOntClinical, fOntGenomic, fClinical, fGenomic, listSensitive, databaseS, false)

	return nil
}

// Client functions
//______________________________________________________________________________________________________________________

func unlynxRequestFromApp(c *cli.Context) error {
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

	data, err := readRequestXMLFrom(os.Stdin)
	if err != nil {
		log.Error("Error while reading from the stdin", err)
		return cli.NewExitError(err, 2)
	}

	errDDT := unlynxDDTRequest(data, os.Stdout, el, entryPointIdx, proofs, false)
	if errDDT != nil {
		errAgg := unlynxAggRequest(data, os.Stdout, el, entryPointIdx, proofs)

		if errAgg != nil {
			log.Error("Error while requesting something...", err)
			return cli.NewExitError(err, 2)
		}
	}

	return nil
}

// read from a reader an xml (until EOF), and unmarshal it
func readRequestXMLFrom(input io.Reader) ([]byte, error) {
	// read from stdin TODO: limit the amount read
	dataBytes, errIo := ioutil.ReadAll(input)

	if errIo != nil {
		log.Error("Error while reading standard input.", errIo)
		return nil, errIo
	}

	log.Info("Correctly read standard input until EOF.")

	return dataBytes, nil
}

//----------------------------------------------------------------------------------------------------------------------
//#----------------------------------------------- DDT REQUEST ---------------------------------------------------------
//----------------------------------------------------------------------------------------------------------------------

// unmarshal the DDTRequest XML
func parserDDTRequestXML(input []byte) (*lib.XMLMedCoDTTRequest, error) {
	// unmarshal xml (assumes bytes are UTF-8 encoded)
	parsedXML := lib.XMLMedCoDTTRequest{}

	errXML := xml.Unmarshal(input, &parsedXML)
	if errXML != nil {
		return nil, errXML
	}

	return &parsedXML, nil
}

// TODO: no log.Fatal in general (this stops immediately)
// TODO: handle errors in to/from bytes in crypto.go
// run DDT of query parameters, all errors will be sent to the output
func unlynxDDTRequest(input []byte, output io.Writer, el *onet.Roster, entryPointIdx int, proofs, testing bool) error {
	start := time.Now()

	// get data from input
	xmlQuery, err := parserDDTRequestXML(input)
	if err != nil {
		return err
	}

	// get formatted data
	encQueryTerms, id, err := xmlQuery.DDTRequestToUnlynxFormat()
	if err != nil {
		log.Error("Error extracing patients data.", err)
		writeDDTResponseXML(output, nil, nil, nil, err)
		return err
	}

	parsingTime := time.Since(start)

	// launch query
	start = time.Now()

	client := serviceMedCo.NewUnLynxClient(el.List[entryPointIdx], strconv.Itoa(entryPointIdx))
	_, result, tr, err := client.SendSurveyDDTRequestTerms(
		el, // Roster
		serviceMedCo.SurveyID(id), // SurveyID
		encQueryTerms,             // Encrypted query terms to tag
		proofs,                    // compute proofs?
		testing,                   // it's for testing
	)

	totalTime := time.Since(start)

	if err != nil {
		log.Error("Error during the DDTRequest service.", err)
		writeDDTResponseXML(output, nil, nil, nil, err)
		return err
	}

	// sanity check
	if len(result) == 0 || len(result) != len(encQueryTerms) {
		log.Error("The number of tags", len(result), "does not match the number of terms", len(encQueryTerms), ".", err)
	}

	tr.DDTRequestTimeCommun = totalTime - tr.DDTRequestTimeExec
	tr.DDTparsingTime = parsingTime
	tr.DDTRequestTimeExec += tr.DDTparsingTime

	err = writeDDTResponseXML(output, xmlQuery, result, &tr, nil)
	if err != nil {
		log.Error("Error while writing result.", err)
		writeDDTResponseXML(output, nil, nil, nil, err)
		return err
	}
	return nil
}

// output result xml on a writer (if result_err != nil, the error is sent)
func writeDDTResponseXML(output io.Writer, xmlQuery *lib.XMLMedCoDTTRequest, result []lib.GroupingKey, tr *serviceMedCo.TimeResults, err error) error {

	/*
		<unlynx_ddt_response>
		    <id>request ID</id>
		    <times unit="ms">{xx: 13, etc}</times>
		    <tagged_values>
			<tagged_value>adfw25e457f=</tagged_value>
			<tagged_value>ADfFD5FDads=</tagged_value>
		    </tagged_values>
		    <error></error>
		</unlynx_ddt_response>
	*/

	resultString := ""
	if err == nil && xmlQuery != nil {
		resultTags := ""

		for _, tag := range result {
			resultTags += "<tagged_value>" + string(tag) + "</tagged_value>"

		}

		resultString = `<unlynx_ddt_response>
					<id>` + (*xmlQuery).QueryID + `</id>
					<times unit="ms">{"DDTRequest execution time":` + strconv.FormatInt(int64(tr.DDTRequestTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"DDTRequest communication time":` + strconv.FormatInt(int64(tr.DDTRequestTimeCommun.Nanoseconds()/1000000.0), 10) +
			`,"DDTRequest parsing time":` + strconv.FormatInt(int64(tr.DDTparsingTime.Nanoseconds()/1000000.0), 10) +
			`}</times>
					<tagged_values>` + resultTags + `</tagged_values>
					<error></error>
				</unlynx_ddt_response>`
	} else if xmlQuery != nil {
		resultString = `<unlynx_ddt_response>
					<id>` + (*xmlQuery).QueryID + `</id>
					<times unit="ms"></times>
					<tagged_values></tagged_values>
					<error>` + err.Error() + `</error>
				</unlynx_ddt_response>`
	} else {
		resultString = `<unlynx_ddt_response>
					<id>unknown</id>
					<times unit="ms"></times>
					<tagged_values></tagged_values>
					<error>` + err.Error() + `</error>
				</unlynx_ddt_response>`
	}

	_, err = io.WriteString(output, resultString)
	if err != nil {
		log.Error("Error while writing DDTResponseXML.", err)
		return err
	}
	return nil
}

//----------------------------------------------------------------------------------------------------------------------
//#----------------------------------------------- AGG REQUEST ---------------------------------------------------------
//----------------------------------------------------------------------------------------------------------------------

// unmarshal the AggRequest XML
func parseAggRequestXML(input []byte) (*lib.XMLMedCoAggRequest, error) {
	// unmarshal xml (assumes bytes are UTF-8 encoded)
	parsedXML := lib.XMLMedCoAggRequest{}
	errXML := xml.Unmarshal(input, &parsedXML)
	if errXML != nil {
		log.Error("Error while unmarshalling AggRequest xml.", errXML)
		return nil, errXML
	}

	return &parsedXML, nil
}

// TODO: no log.Fatal in general (this stops immediately)
// TODO: handle errors in to/from bytes in crypto.go
// run aggregation of the results (and remaining protocols), all errors will be sent to the output
func unlynxAggRequest(input []byte, output io.Writer, el *onet.Roster, entryPointIdx int, proofs bool) error {
	start := time.Now()

	// get data from input
	xmlQuery, err := parseAggRequestXML(input)
	if err != nil {
		return err
	}

	// get formatted data
	encDummyFlags, id, err := xmlQuery.AggRequestToUnlynxFormat()
	if err != nil {
		log.Error("Error extracing patients data.", err)
		writeAggResponseXML(output, nil, nil, nil, err)
		return err
	}

	parsingTime := time.Since(start)

	// locally aggregate results
	start = time.Now()
	aggregate := LocalAggregate(encDummyFlags, el.Aggregate)
	aggregationTime := time.Since(start)

	// launch query
	start = time.Now()

	cPK, err := lib.DeserializePoint(xmlQuery.ClientPubKey)
	if err != nil {
		log.Error("Error decoding client public key.", err)
		writeAggResponseXML(output, nil, nil, nil, err)
		return err
	}

	client := serviceMedCo.NewUnLynxClient(el.List[entryPointIdx], strconv.Itoa(entryPointIdx))
	_, result, tr, err := client.SendSurveyAggRequest(
		el, // Roster
		serviceMedCo.SurveyID(id), // SurveyID
		cPK,        // client public key
		*aggregate, // Encrypted local aggregation result
		proofs,     // compute proofs?
	)

	totalTime := time.Since(start)

	if err != nil {
		log.Error("Error during the DDTRequest service.", err)
		writeAggResponseXML(output, nil, nil, nil, err)
		return err
	}

	tr.AggRequestTimeCommun = totalTime - tr.DDTRequestTimeExec
	tr.LocalAggregationTime = aggregationTime
	tr.AggParsingTime = parsingTime
	tr.AggRequestTimeExec += tr.AggParsingTime + tr.LocalAggregationTime

	err = writeAggResponseXML(output, xmlQuery, &result, &tr, nil)
	if err != nil {
		log.Error("Error while writing result.", err)
		writeAggResponseXML(output, nil, nil, nil, err)
		return err
	}
	return nil
}

// LocalAggregate locally aggregates the encrypted dummy flags
func LocalAggregate(encDummyFlags lib.CipherVector, pubKey abstract.Point) *lib.CipherText {
	// there are no results
	if len(encDummyFlags) == 0 {
		return lib.EncryptInt(pubKey, int64(0))
	}

	result := &encDummyFlags[0]
	for i := 1; i < len(encDummyFlags); i++ {
		result.Add(*result, encDummyFlags[i])
	}

	return result
}

// output result xml on a writer (if result_err != nil, the error is sent)
func writeAggResponseXML(output io.Writer, xmlQuery *lib.XMLMedCoAggRequest, aggregate *lib.CipherText, tr *serviceMedCo.TimeResults, err error) error {

	/*
		<unlynx_agg_response>
		    <id>request ID</id>
		    <times>{cc: 55}</times>
		    <aggregate>f85as4fas57f=</aggregate>
		    <error></error>
		</unlynx_agg_response>
	*/

	resultString := ""
	if err == nil && xmlQuery != nil {
		resultString = `<unlynx_agg_response>
					<id>` + (*xmlQuery).QueryID + `</id>
					<times unit="ms">{"AggRequest execution time":` + strconv.FormatInt(int64(tr.AggRequestTimeExec.Nanoseconds()/1000000.0), 10) +
			`,"AggRequest communication time":` + strconv.FormatInt(int64(tr.AggRequestTimeCommun.Nanoseconds()/1000000.0), 10) +
			`,"AggRequest parsing time":` + strconv.FormatInt(int64(tr.AggParsingTime.Nanoseconds()/1000000.0), 10) +
			`,"AggRequest aggregation time":` + strconv.FormatInt(int64(tr.LocalAggregationTime.Nanoseconds()/1000000.0), 10) +
			`}</times>
					<aggregate>` + aggregate.Serialize() + `</aggregate>
					<error></error>
				</unlynx_agg_response>`
	} else if xmlQuery != nil {
		resultString = `<unlynx_agg_response>
					<id>` + (*xmlQuery).QueryID + `</id>
					<times></times>
		    			<aggregate></aggregate>
					<error>` + err.Error() + `</error>
				</unlynx_agg_response>`
	} else {
		resultString = `<unlynx_agg_response>
					<id>unknown</id>
					<times></times>
		    			<aggregate></aggregate>
					<error>` + err.Error() + `</error>
				</unlynx_agg_response>`
	}

	_, err = io.WriteString(output, resultString)
	if err != nil {
		log.Error("Error while writing AggResponseXML.", err)
		return err
	}
	return nil
}