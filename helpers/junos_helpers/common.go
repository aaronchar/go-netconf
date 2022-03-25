package junos_helpers

import (
	"bufio"
	"io/ioutil"
	"strings"

	"github.com/antchfx/xmlquery"
	"golang.org/x/crypto/ssh"
)

type NCClient interface {
	Close() error
	ReadGroup(applygroup string) (string, error)
	UpdateRawConfig(applygroup string, netconfcall string, commit bool) (string, error)
	DeleteConfig(applygroup string) (string, error)
	DeleteConfigNoCommit(applygroup string) (string, error)
	SendCommit() error
	MarshalGroup(id string, obj interface{}) error
	SendTransaction(id string, obj interface{}, commit bool) error
	SendRawConfig(netconfcall string, commit bool) (string, error)
	ReadRawGroup(applygroup string) (string, error)
}

// parseGroupData is a function that cleans up the returned data for generic config groups
func parseGroupData(input string) (reply string, err error) {
	var cfgSlice []string

	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		cfgSlice = append(cfgSlice, scanner.Text())
	}

	var cfgSlice2 []string

	for _, v := range cfgSlice {
		r := strings.NewReplacer("}\\n}\\n", "}", "\\n", "", "/*", "", "*/", "", "</configuration-text>", "")

		d := r.Replace(v)

		// fmt.Println(d)

		cfgSlice2 = append(cfgSlice2, d)
	}

	// Figure out the offset. Each Junos version could give us different stuff, so let's look for the group name
	begin := 0
	end := 0

	for k, v := range cfgSlice2 {
		if v == "groups" {
			begin = k + 4
			break
		}
	}

	// We don't want the very end slice due to config terminations we don't need.
	end = len(cfgSlice2) - 3

	// fmt.Printf("Begin = %v\nEnd = %v\n", begin, end)

	reply = strings.Join(cfgSlice2[begin:end], " ")

	return reply, nil
}
func publicKeyFile(file string) ssh.AuthMethod {
	buffer, err := ioutil.ReadFile(file)
	if err != nil {
		return nil
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(key)
}
func findGroupInDoc(payload string, search string) ([]*xmlquery.Node, error) {
	doc, err := xmlquery.Parse(strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	// reply will contain all the groups that have been provisioned on the device
	// we now need to find the one related to this exact reference
	nodes, err := xmlquery.QueryAll(doc, search)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}
