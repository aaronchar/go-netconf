package junos_helpers

import (
	"fmt"
	"github.com/antchfx/xmlquery"
	"log"
	"strings"
	"sync"

	driver "github.com/davedotdev/go-netconf/drivers/driver"
	sshdriver "github.com/davedotdev/go-netconf/drivers/ssh"
	"golang.org/x/crypto/ssh"
)

const batchGetGroupStr = `<get-configuration database="committed" format="text" >
<configuration>
<groups></groups>
</configuration>
</get-configuration>
`
const batchGetGroupXMLStr = `<get-configuration>
  <configuration>
  <groups></groups>
  </configuration>
</get-configuration>
`

// BatchGoNCClient type for storing data and wrapping functions
type BatchGoNCClient struct {
	Driver    driver.Driver
	Lock      sync.RWMutex
	readCache string
}

// Close is a functional thing to close the Driver
func (g *BatchGoNCClient) Close() error {
	g.Driver = nil
	return nil
}

// ReadGroup is a helper function
func (g *BatchGoNCClient) ReadGroup(applygroup string) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// UpdateRawConfig deletes group data and replaces it (for Update in TF)
func (g *BatchGoNCClient) UpdateRawConfig(applygroup string, netconfcall string, commit bool) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// DeleteConfig is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) DeleteConfig(applygroup string) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// DeleteConfigNoCommit is a wrapper for driver.SendRaw()
// Does not provide mandatory commit unlike DeleteConfig()
func (g *BatchGoNCClient) DeleteConfigNoCommit(applygroup string) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// SendCommit is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendCommit() error {
	g.Lock.Lock()
	g.Lock.Unlock()
	return nil
}

// MarshalGroup accepts a struct of type X and then marshals data onto it
func (g *BatchGoNCClient) MarshalGroup(id string, obj interface{}) error {

	reply, err := g.ReadRawGroup(id)
	if err != nil {
		return err
	}
	doc, err := xmlquery.Parse(strings.NewReader(reply))
	if err != nil {
		panic(err)
	}
	// reply will contain all the groups that have been provisioned on the device
	// we now need to find the one related to this exact reference
	return nil
}

// SendTransaction is a method that unnmarshals the XML, creates the transaction and passes in a commit
func (g *BatchGoNCClient) SendTransaction(id string, obj interface{}, commit bool) error {
	return nil
}

// SendRawConfig is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendRawConfig(netconfcall string, commit bool) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// ReadRawGroup is a helper function
func (g *BatchGoNCClient) ReadRawGroup(applygroup string) (string, error) {

	g.Lock.Lock()
	output := g.readCache
	// We only want to capture all the groups the first execution
	// after that we depend up on this cache for the duration of the execution
	if g.readCache == "" {
		if err := g.Driver.Dial(); err != nil {
			log.Fatal(err)
		}
		reply, err := g.Driver.SendRaw(batchGetGroupXMLStr)
		if err != nil {
			errInternal := g.Driver.Close()
			g.Lock.Unlock()
			return "", fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		if err := g.Driver.Close(); err != nil {
			return "", err
		}
		g.readCache = reply.Data
		output = g.readCache
		println(output)
	}
	g.Lock.Unlock()
	return output, nil
}

// NewClient returns gonetconf new client driver
func NewBatchClient(username string, password string, sshkey string, address string, port int) (NCClient, error) {

	// Dummy interface var ready for loading from inputs
	var nconf driver.Driver

	d := driver.New(sshdriver.New())

	nc := d.(*sshdriver.DriverSSH)

	nc.Host = address
	nc.Port = port

	// SSH keys takes priority over password based
	if sshkey != "" {
		nc.SSHConfig = &ssh.ClientConfig{
			User: username,
			Auth: []ssh.AuthMethod{
				publicKeyFile(sshkey),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}
	} else {
		// Sort yourself out with SSH. Easiest to do that here.
		nc.SSHConfig = &ssh.ClientConfig{
			User:            username,
			Auth:            []ssh.AuthMethod{ssh.Password(password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}
	}

	nconf = nc

	return &BatchGoNCClient{Driver: nconf}, nil
}
