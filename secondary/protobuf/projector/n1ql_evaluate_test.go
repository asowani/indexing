package protobuf

import (
	"compress/bzip2"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// TODO:
//  1. add benchmark for different size of documents (small, medium, large)
//     and complex expressions.

var testdata = "../../testdata"
var usersBzip2 = filepath.Join(testdata, "users.json.bz2")
var projectsBzip2 = filepath.Join(testdata, "projects.json.bz2")

var doc150 = []byte(`{ "type": "user", "first-name": "Daniel", "last-name": "Fred", "age": 32, "emailid": "Daniel@gmail.com", "city": "Kathmandu", "gender": "female" }`)
var doc2000 = []byte(
	`{"age": 63, "city": "Kathmandu",
"obbligato":{"age": 38, 
"evaporable":{"age": 46, 
"Holothuridea":[[{"age": 50, 
"limeade":[{"age": 42, 
"Doug":null,"Zabian":4.280574703284054}],"whitefishery":54.124022995116874,"drochuil":[true,{"age": 45, 
"trophosphere":"dreamsiness","unwired":"inspirator","parochialist":null},["spoonmaking"],false,{"age": 16, 
"retractile":{"age": 51, 
"panatrophy":[null,[10.579402365014104,[[{"age": 18, 
"Charonian":"subspontaneous"}]],{"age": 32, 
"pock":"Aviculidae","piezoelectricity":[[null],"decision"],"triverbal":false,"Irvingesque":false,"Nheengatu":68.94658039778236,"Phororhacidae":"matins","euhemerism":[66.43333771445981,null],"sinful":[{"age": 47, 
"nonsilicated":{"age": 68, 
"taccada":60.329671903440165},"Hibernicism":{"age": 37, 
"integrable":{"age": 69, 
"stringiness":null,"accoy":65.22961374836026}},"transportable":{"age": 35, 
"scuppler":[43.57496753628395,88.19284469483115]},"orbitary":65.5529496197739,"tithonometer":"encomia","beater":[["haulmy",null],"misnavigation",[{"age": 41, 
"abrasive":{"age": 19, 
"bromeliaceous":"concause","gutte":null,"underborn":false},"prejudger":{"age": 76, 
"pagrus":"Oreodontidae","compromission": 20 },"chrysophilist":null,"biloculine":36.858326074373885,"unjesting":false,"annaline":{"age": 64, 
"monotype":{"age": 59, 
"reposefulness":66.99562477978547},"dipeptid":50.929176372173465},"nauther":[["conine"]]}],false]},"causer"]}],41.69318326741408,"unhumanness"]},"teaselwort":false},{"age": 74, 
"restrip":null,"eustachium":"unliable"},60.61783258512191],"disilicid":[[{"age": 25, 
"devitrify":true}],"interferant",43.34210006781631],"ligroine":{"age": 39, 
"championship":{"age": 25, 
"ebenaceous":"agalite","creatable":false}}},76.3274439158485]],"Cocceianism":null,"intercurrence":"pyribole"},"blissless":null,"indevoutly":16.193431336869093,"Labidura":{"age": 60, 
"horizontalize":57.07719697617387,"monotropaceous":true},"unalone":3.4689803154751875}}`)

func init() {
	fnames := []string{usersBzip2, projectsBzip2}

	for _, fname := range fnames {
		var ds []interface{}

		if f, err := os.Open(fname); err != nil {
			panic(err)
		} else {
			r := bzip2.NewReader(f)
			if data, err := ioutil.ReadAll(r); err != nil {
				panic(err)
			} else if err = json.Unmarshal(data, &ds); err != nil {
				panic(err)
			}
			f.Close()
		}
	}
}

func TestN1QLTransform150(t *testing.T) {
	cExprs, err := CompileN1QLExpression([]string{`city`, `age`})
	if err != nil {
		t.Fatal(err)
	}
	secKey, err := N1QLTransform([]byte("docid"), doc150, cExprs)
	if err != nil {
		t.Fatal(err)
	}
	if string(secKey) != `["Kathmandu",32,"docid"]` {
		t.Fatalf("evaluation failed %v", string(secKey))
	}
}

func TestN1QLTransform2000(t *testing.T) {
	cExprs, err := CompileN1QLExpression([]string{`city`, `age`})
	if err != nil {
		t.Fatal(err)
	}
	secKey, err := N1QLTransform([]byte("docid"), doc2000, cExprs)
	if err != nil {
		t.Fatal(err)
	}
	if string(secKey) != `["Kathmandu",63,"docid"]` {
		t.Fatalf("evaluation failed %v", string(secKey))
	}
}

func BenchmarkCompileN1QLExpression(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CompileN1QLExpression([]string{`age`})
	}
}

func BenchmarkN1QLTransform150(b *testing.B) {
	cExprs, _ := CompileN1QLExpression([]string{`age`})
	for i := 0; i < b.N; i++ {
		N1QLTransform([]byte("docid"), doc150, cExprs)
	}
}

func BenchmarkN1QLTransform2000(b *testing.B) {
	cExprs, _ := CompileN1QLExpression([]string{`age`})
	for i := 0; i < b.N; i++ {
		N1QLTransform([]byte("docid"), doc2000, cExprs)
	}
}
