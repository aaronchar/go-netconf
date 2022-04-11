package junos_helpers

import (
	"encoding/xml"
	"fmt"
	"strings"
	"sync"

	driver "github.com/davedotdev/go-netconf/drivers/driver"
	sshdriver "github.com/davedotdev/go-netconf/drivers/ssh"
	"golang.org/x/crypto/ssh"
)

const batchGroupStrXML = `<load-configuration action="merge" format="xml">
<configuration>
%s
</configuration>
</load-configuration>
`
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
const batchValidateCandidate = `<validate> 
<source> 
	<candidate/> 
</source> 
</validate>`
const batchReadWrapper = `<configuration>%s</configuration>`

const batchCommitStr = `<commit/>`

const batchDeletePayload = `<groups operation="delete">
	<name>%[1]s</name>
</groups>
<apply-groups operation="delete">%[1]s</apply-groups>`

const batchDeleteStr = `<edit-config>
	<target>
		<candidate/>
	</target>
	<default-operation>none</default-operation> 
	<config>
		<configuration>
			%s
		</configuration>
	</config>
</edit-config>`

var (
	batchConfigReplacer = strings.NewReplacer("<configuration>", "", "</configuration>", "")
)

// BatchGoNCClient type for storing data and wrapping functions
type BatchGoNCClient struct {
	Driver driver.Driver
	Lock   sync.RWMutex
	BH     BatchHelper
	// readCache   *sync.Map
	// writeCache  *sync.Map
	// deleteCache string
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
	// we are filling up the read buffer, this will only be done once regardless of the amount of \
	g.Lock.Lock()
	defer g.Lock.Unlock()
	if err := g.BH.AddToDeleteMap(applygroup); err != nil {
		return "", err
	}
	if err := g.BH.AddToWriteMap(netconfcall); err != nil {
		return "", err
	}

	return "", nil
}

// DeleteConfig is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) DeleteConfig(applygroup string) (string, error) {
	g.Lock.Lock()
	defer g.Lock.Unlock()

	return "", nil
}

// DeleteConfigNoCommit is a wrapper for driver.SendRaw()
// Does not provide mandatory commit unlike DeleteConfig()
func (g *BatchGoNCClient) DeleteConfigNoCommit(applygroup string) (string, error) {
	g.Lock.Lock()
	defer g.Lock.Unlock()
	if err := g.BH.AddToDeleteMap(applygroup); err != nil {
		return "", err
	}
	return "", nil
}

// SendCommit is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendCommit() error {

	g.Lock.Lock()
	defer g.Lock.Unlock()

	deleteCache := g.BH.QueryAllGroupDeletes()
	writeCache := g.BH.QueryAllGroupWrites()
	if err := g.Driver.Dial(); err != nil {
		return err
	}

	if deleteCache != "" {
		backDeleteString := fmt.Sprintf(batchDeleteStr, deleteCache)
		// So on the commit we are going to send our entire delete-cache, if we get any load error
		// we return the full xml error response and exit
		batchDeleteReply, err := g.Driver.SendRaw(backDeleteString)
		if err != nil {
			errInternal := g.Driver.Close()
			return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		// I am doing string checks simply because it is most likely more efficient
		// than loading in through an xml parser
		if strings.Contains(batchDeleteReply.Data, "operation-failed") {
			return fmt.Errorf("failed to write batch delete %s", batchDeleteReply.Data)
		}
	}
	if writeCache != "" {

		groupCreateString := fmt.Sprintf(batchGroupStrXML, writeCache)
		// So on the commit we are going to send our entire write-cache, if we get any load error
		// we return the full xml error response and exit
		batchWriteReply, err := g.Driver.SendRaw(groupCreateString)
		if err != nil {
			errInternal := g.Driver.Close()
			return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		// I am doing string checks simply because it is most likely more efficient
		// than loading in through an xml parser
		if strings.Contains(batchWriteReply.Data, "operation-failed") {
			return fmt.Errorf("failed to write batch configuration %s", batchWriteReply.Data)
		}
	}
	// we have loaded the full configuration without any error
	// before we can commit this we are going to do a commit check
	// if it fails we return the full xml error
	commitCheckReply, err := g.Driver.SendRaw(batchValidateCandidate)
	if err != nil {
		errInternal := g.Driver.Close()
		return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
	}

	// I am doing string checks simply because it is most likely more efficient
	// than loading in through an xml parser
	if !strings.Contains(commitCheckReply.Data, "commit-check-success") {
		return fmt.Errorf("candidate commit check failed %s", commitCheckReply.Data)
	}
	if _, err = g.Driver.SendRaw(batchCommitStr); err != nil {
		return err
	}
	return nil
}

// MarshalGroup accepts a struct of type X and then marshals data onto it
func (g *BatchGoNCClient) MarshalGroup(id string, obj interface{}) error {
	reply, err := g.ReadRawGroup(id)
	if err != nil {
		return err
	}
	err = xml.Unmarshal([]byte(reply), &obj)
	if err != nil {
		return err
	}
	return nil
}

// SendTransaction is a method that unnmarshals the XML, creates the transaction and passes in a commit
func (g *BatchGoNCClient) SendTransaction(id string, obj interface{}, commit bool) error {
	jconfig, err := xml.Marshal(obj)

	if err != nil {
		return err
	}
	if id != "" {
		_, err = g.UpdateRawConfig(id, string(jconfig), commit)
	} else {
		if _, err := g.SendRawConfig(string(jconfig), commit); err != nil {
			return err
		}
	}
	return nil
}

// SendRawConfig is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendRawConfig(netconfcall string, commit bool) (string, error) {
	g.Lock.Lock()
	defer g.Lock.Unlock()

	g.BH.AddToWriteMap(netconfcall)
	return "", nil
}

// ReadRawGroup is a helper function
func (g *BatchGoNCClient) ReadRawGroup(applygroup string) (string, error) {

	g.Lock.Lock()
	defer g.Lock.Unlock()

	if !g.BH.IsHydrated() {
		if err := g.Driver.Dial(); err != nil {
			errInternal := g.Driver.Close()
			return "", fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		reply, err := g.Driver.SendRaw(batchGetGroupXMLStr)
		if err != nil {
			errInternal := g.Driver.Close()
			return "", fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		if err := g.Driver.Close(); err != nil {
			return "", err
		}
		g.BH.AddToReadMap(reply.Data)
	}
	return g.BH.QueryGroupXMLFromCache(applygroup)
}

// This is called on instantion of the batch client, it is used to hydrate
// the read cahce.
func (g *BatchGoNCClient) hydrateReadCache() error {
	if err := g.Driver.Dial(); err != nil {
		errInternal := g.Driver.Close()
		return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
	}
	reply, err := g.Driver.SendRaw(batchGetGroupXMLStr)
	if err != nil {
		errInternal := g.Driver.Close()
		return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
	}
	if err := g.Driver.Close(); err != nil {
		return err
	}
	g.BH.AddToReadMap(reply.Data)
	return nil
}

// NewBatchClient returns gonetconf new client driver
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
	bh := NewBatchHelper()

	c := &BatchGoNCClient{
		Driver: nconf,
		BH:     bh,
	}

	// // we are hydrating the read cache on client creation, this should save time during the Terrafrom calls process
	// if err := c.hydrateReadCache(); err != nil {
	// 	return nil, err
	// }
	return c, nil
}
