package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/cmd/gaia/app"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	stypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/peterhellberg/link"
)

var (
	ghapi string
	base  = "https://api.github.com/repos/cosmos/launch"
	cdc   *codec.Codec
)

func init() {
	cdc = app.MakeCodec()
	ghapi = os.Getenv("GITHUB_API_TOKEN")
}

func pullsURL() string {
	return fmt.Sprintf("%s/pulls?access_token=%s", base, ghapi)
}

func pullFilesURL(number int) string {
	return fmt.Sprintf("%s/pulls/%d/files?access_token=%s", base, number, ghapi)
}

func main() {
	fmt.Println("Fetching Pull Request Records...")
	pulls, err := fetchPulls()
	if err != nil {
		panic(err)
	}
	fmt.Println("Fetching and validatating gentxs...")
	var gentxs GenTxs
	for _, pull := range pulls {
		if !pull.Labels.isgentx() {
			continue
		}
		files, err := fetchFiles(pull.Number)
		if err != nil {
			panic(err)
		}
		if files.valid() {
			gentx, err := fetchRaw(files[0].RawURL)
			if err != nil {
				panic(err)
			}
			gentxs = append(gentxs, gentx)
		}
	}
	fmt.Println("Validator Allocations:")
	gentxs.print()
	fmt.Printf("Number of Submissions %d\n", len(gentxs))
	gentxs.total()
}

// GenTxs does stuff
type GenTxs []stypes.MsgCreateValidator

func (gtx GenTxs) print() {
	for _, gt := range gtx {
		fmt.Printf("  %-35s:  %s\n", gt.Description.Moniker, uatomToAtom(gt.Value))
	}
}

func (gtx GenTxs) total() {
	out := sdk.NewInt64Coin("atom", int64(0))
	for _, gt := range gtx {
		out = out.Add(uatomToAtom(gt.Value))
	}
	fmt.Println(out)
}

func uatomToAtom(c sdk.Coin) sdk.Coin {
	return sdk.NewCoin("atom", c.Amount.Quo(sdk.NewInt(1000000)))
}

func pulls(resp []pullsResponse, url string) (out []pullsResponse, next string) {
	res, err := http.Get(url)
	if err != nil {
		return
	}
	pr, err := processPullResponse(res)
	if err != nil {
		panic(err)
	}
	out = append(resp, pr)

	rg := link.ParseResponse(res)
	if val, ok := rg["next"]; ok {
		return out, val.URI
	}
	return out, ""
}

func fetchPulls() (pr pullsResponse, err error) {
	responses := []pullsResponse{}
	url := pullsURL()
	for {
		responses, url = pulls(responses, url)
		if url == "" {
			break
		}
	}

	var out pullsResponse
	for _, r := range responses {
		for _, p := range r {
			out = append(out, p)
		}
	}
	return out, nil
}

func processPullResponse(res *http.Response) (pr pullsResponse, err error) {
	bz, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bz, &pr); err != nil {
		var rl ratelimit
		if err = json.Unmarshal(bz, &rl); err == nil {
			fmt.Println("time till rate limit reset", left(res.Header.Get("X-RateLimit-Reset")))
			os.Exit(1)
		}
		return
	}
	return
}

func left(s string) time.Duration {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(err)
	}
	return time.Unix(i, 0).Sub(time.Now())
}

func fetchFiles(pull int) (f files, err error) {
	res, err := http.Get(pullFilesURL(pull))
	if err != nil {
		return
	}
	bz, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}
	if err = json.Unmarshal(bz, &f); err != nil {
		return
	}
	return
}

func fetchRaw(url string) (gtx stypes.MsgCreateValidator, err error) {
	res, err := http.Get(url)
	if err != nil {
		return
	}
	bz, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}

	var stdTx auth.StdTx
	if err = cdc.UnmarshalJSON(bz, &stdTx); err != nil {
		return
	}

	if len(stdTx.Msgs) != 1 {
		return
	}

	gentx := stdTx.Msgs[0]

	if err = gentx.ValidateBasic(); err != nil {
		return
	}

	if jsn, err := cdc.MarshalJSON(gentx); err == nil {
		cdc.UnmarshalJSON(jsn, &gtx)
	}

	return
}

type ratelimit struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url"`
}

func (rl ratelimit) hit() bool {
	if rl.DocumentationURL == "https://developer.github.com/v3/#rate-limiting" {
		return true
	}
	return false
}
