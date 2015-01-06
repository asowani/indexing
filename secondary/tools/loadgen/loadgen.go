// Tool generates JSON document using monster productions.
// * productions are defined for `default`, `users` and `projects` bucket.
// * parallel load can be generated using `-par` switch.
// * `-count` switch specify no. of documents to be generated by each routine.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/couchbase/indexing/secondary/dcp"
	parsec "github.com/prataprc/goparsec"
	"github.com/prataprc/monster"
	mcommon "github.com/prataprc/monster/common"
)

var options struct {
	seed     int      // seed for monster tool
	buckets  []string // buckets to populate
	prods    []string
	parallel int // number of parallel routines per bucket
	count    int // number of documents to be generated per routine
	expiry   int // set expiry for the document, in seconds
}

var testDir string
var bagDir string
var done = make(chan bool, 16)

func argParse() string {
	var buckets, prods string

	seed := time.Now().UTC().Second()
	flag.IntVar(&options.seed, "seed", seed,
		"seed for monster tool")
	flag.StringVar(&buckets, "buckets", "default",
		"buckets to populate")
	flag.StringVar(&prods, "prods", "users.prod",
		"command separated list of production files for each bucket")
	flag.IntVar(&options.parallel, "par", 1,
		"number of parallel routines per bucket")
	flag.IntVar(&options.count, "count", 0,
		"number of documents to be generated per routine")
	flag.IntVar(&options.expiry, "expiry", 0,
		"expiry duration for a document (TTL)")

	flag.Parse()

	options.buckets = strings.Split(buckets, ",")
	options.prods = strings.Split(prods, ",")

	// collect production files.
	_, filename, _, _ := runtime.Caller(1)
	testDir = path.Join(path.Dir(path.Dir(path.Dir(filename))), "tests/testdata")
	bagDir = testDir

	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}
	return args[0]
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage : %s [OPTIONS] <cluster-addr> \n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	cluster := argParse()
	if !strings.HasPrefix(cluster, "http://") {
		cluster = "http://" + cluster
	}

	n := 0
	for i, bucket := range options.buckets {
		prodfile := getProdfilePath(options.prods[i])
		n += loadBucket(cluster, bucket, prodfile, options.count)
	}
	for n > 0 {
		<-done
		n--
	}
}

func loadBucket(cluster, bucket, prodfile string, count int) int {
	u, err := url.Parse(cluster)
	mf(err, "parse")

	c, err := couchbase.Connect(u.String())
	mf(err, "connect - "+u.String())

	p, err := c.GetPool("default")
	mf(err, "pool")

	bs := make([]*couchbase.Bucket, 0, options.parallel)
	for i := 0; i < options.parallel; i++ {
		b, err := p.GetBucket(bucket)
		mf(err, "bucket")
		bs = append(bs, b)
		go genDocuments(b, prodfile, i+1, options.count)
	}
	return options.parallel
}

func genDocuments(b *couchbase.Bucket, prodfile string, idx, n int) {
	text, err := ioutil.ReadFile(prodfile)
	s := parsec.NewScanner(text)
	root, _ := monster.Y(s)
	scope := root.(mcommon.Scope)
	nterms := scope["_nonterminals"].(mcommon.NTForms)
	scope = monster.BuildContext(scope, uint64(options.seed), bagDir)

	for i := 0; i < n; i++ {
		doc := monster.EvalForms("root", scope, nterms["s"]).(string)
		key := fmt.Sprintf("%s-%v-%v", b.Name, idx, i+1)
		err = b.SetRaw(key, options.expiry, []byte(doc))
		if err != nil {
			fmt.Printf("%T %v\n", err, err)
		}
		mf(err, "error setting document")
	}
	fmt.Printf("routine %v generated %v documents for %q\n", idx, n, b.Name)
	done <- true
}

func mf(err error, msg string) {
	if err != nil {
		log.Fatalf("%v: %v", msg, err)
	}
}

func getProdfilePath(name string) string {
	return path.Join(testDir, name)
}
