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
	Driver      driver.Driver
	Lock        sync.RWMutex
	readCache   string
	writeCache  string
	deleteCache string
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
	defer g.Lock.Unlock()

	g.cacheAddToDelete(applygroup)
	g.cacheAddToWrite(netconfcall)
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

	// Due to the way this functions any resource deletes will only happen client site
	// however a full destroy will be committed to the server.
	g.cacheAddToDelete(applygroup)
	return "", nil
}

// SendCommit is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendCommit() error {

	g.Lock.Lock()
	defer g.Lock.Unlock()

	deleteCache := g.cacheReadFromDelete()
	writeCache := g.cacheReadFromWrite()
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
	nodeXML, err := g.getReadXMLFromCaches(id)
	if err != nil {
		return err
	}
	if err := xml.Unmarshal([]byte(nodeXML), &obj); err != nil {
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

	g.cacheAddToWrite(netconfcall)
	return "", nil
}

// ReadRawGroup is a helper function
func (g *BatchGoNCClient) ReadRawGroup(applygroup string) (string, error) {

	g.Lock.Lock()
	defer g.Lock.Unlock()

	output := g.cacheReadFromRemote()
	// We only want to capture all the groups the first execution
	// after that we depend up on this cache for the duration of the execution
	if output == "" {
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
		g.cacheAddToRemoteRead(reply.Data)
		output = g.cacheReadFromRemote()
	}
	return output, nil
}

func (g *BatchGoNCClient) cacheAddToRemoteRead(in string) {
	g.readCache = in
}
func (g *BatchGoNCClient) cacheAddToWrite(in string) {
	// we need to strip off the <configuration> blocks since we want to send this \
	// as one large configuration push without changing the way the upstream system works
	g.writeCache += batchConfigReplacer.Replace(in)
}
func (g *BatchGoNCClient) cacheAddToDelete(in string) {
	g.deleteCache += fmt.Sprintf(batchDeletePayload, in)
}
func (g *BatchGoNCClient) cacheReadFromWrite() string {
	return g.writeCache
}
func (g *BatchGoNCClient) cacheReadFromRemote() string {
	return g.readCache
}
func (g *BatchGoNCClient) cacheReadFromDelete() string {
	return g.deleteCache
}
func (g *BatchGoNCClient) getReadXMLFromCaches(id string) (string, error) {
	// Because of the order of operation we are always going to have to read from the /
	// write-cache first since that will have our latest state. If an element does not exist /
	// in the write-cache we will then check read cache which has the current remote state.
	var nodeXML string
	writeNodes, err := findGroupInDoc(g.cacheReadFromWrite(), fmt.Sprintf("//groups[name='%s']", id))
	if err != nil {
		return nodeXML, err
	}
	if len(writeNodes) > 1 {
		return nodeXML, fmt.Errorf("%s returned an invalid read cache node count %d", id, len(writeNodes))
	} else if len(writeNodes) == 0 {
		// we don't have anything in our write-cache for this apply-group. So we are going to check the
		// reply-cache which contains the latest remote state.
		replyCache, err := g.ReadRawGroup(id)
		if err != nil {
			return nodeXML, err
		}
		subNodes, err := findGroupInDoc(replyCache, fmt.Sprintf("//groups[name='%s']", id))
		if err != nil {
			return nodeXML, err
		}
		if len(subNodes) > 1 || len(subNodes) == 0 {
			return nodeXML, fmt.Errorf("%s returned an invalid write cache node count %d", id, len(subNodes))
		}
		nodeXML = fmt.Sprintf(batchReadWrapper, subNodes[0].OutputXML(true))
	} else {
		nodeXML = fmt.Sprintf(batchReadWrapper, writeNodes[0].OutputXML(true))
	}
	return nodeXML, nil
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

	return &BatchGoNCClient{Driver: nconf}, nil
}
