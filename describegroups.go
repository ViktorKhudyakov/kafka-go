package kafka

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"

	"github.com/segmentio/kafka-go/protocol/describegroups"
)

type DescribeGroupsRequest struct {
	// Address of the kafka broker to send the request to.
	Addr     net.Addr
	GroupIDs []string
}

type DescribeGroupsResponse struct {
	Groups []DescribeGroupsResponseGroup
}

type DescribeGroupsResponseGroup struct {
	GroupID    string
	GroupState string
	Members    []DescribeGroupsResponseMember
}

// MemberInfo represents the membership information for a single group member.
type DescribeGroupsResponseMember struct {
	MemberID          string
	ClientID          string
	ClientHost        string
	MemberMetadata    DescribeGroupsResponseMemberMetadata
	MemberAssignments DescribeGroupsResponseAssignments
}

// GroupMemberMetadata stores metadata associated with a group member.
type DescribeGroupsResponseMemberMetadata struct {
	Version  int16
	Topics   []string
	UserData []byte
}

// GroupMemberAssignmentsInfo stores the topic partition assignment data for a group member.
type DescribeGroupsResponseAssignments struct {
	Version  int16
	Topics   []GroupMemberTopic
	UserData []byte
}

// GroupMemberTopic is a mapping from a topic to a list of partitions in the topic. It is used
// to represent the topic partitions that have been assigned to a group member.
type GroupMemberTopic struct {
	Topic      string
	Partitions []int32
}

func (c *Client) DescribeGroup(
	ctx context.Context,
	req DescribeGroupsRequest,
) (*DescribeGroupsResponse, error) {
	protoResp, err := c.roundTrip(
		ctx,
		req.Addr,
		&describegroups.Request{
			Groups: req.GroupIDs,
		},
	)
	if err != nil {
		return nil, err
	}
	apiResp := protoResp.(*describegroups.Response)
	resp := &DescribeGroupsResponse{
		Groups: []DescribeGroupsResponseGroup{},
	}

	for _, apiGroup := range apiResp.Groups {
		group := DescribeGroupsResponseGroup{
			GroupID:    apiGroup.GroupID,
			GroupState: apiGroup.GroupState,
			Members:    []DescribeGroupsResponseMember{},
		}

		for _, member := range apiGroup.Members {
			decodedMetadata, err := decodeMemberMetadata(member.MemberMetadata)
			if err != nil {
				return nil, err
			}
			decodedAssignments, err := decodeMemberAssignments(member.MemberAssignment)
			if err != nil {
				return nil, err
			}

			group.Members = append(group.Members, DescribeGroupsResponseMember{
				MemberID:          member.MemberID,
				ClientID:          member.ClientID,
				ClientHost:        member.ClientHost,
				MemberAssignments: decodedAssignments,
				MemberMetadata:    decodedMetadata,
			})
		}
		resp.Groups = append(resp.Groups, group)
	}

	return resp, nil
}

// See http://kafka.apache.org/protocol.html#The_Messages_DescribeGroups
type describeGroupsRequestV0 struct {
	// List of groupIds to request metadata for (an empty groupId array
	// will return empty group metadata).
	GroupIDs []string
}

func (t describeGroupsRequestV0) size() int32 {
	return sizeofStringArray(t.GroupIDs)
}

func (t describeGroupsRequestV0) writeTo(wb *writeBuffer) {
	wb.writeStringArray(t.GroupIDs)
}

type describeGroupsResponseMemberV0 struct {
	// MemberID assigned by the group coordinator
	MemberID string

	// ClientID used in the member's latest join group request
	ClientID string

	// ClientHost used in the request session corresponding to the member's
	// join group.
	ClientHost string

	// MemberMetadata the metadata corresponding to the current group protocol
	// in use (will only be present if the group is stable).
	MemberMetadata []byte

	// MemberAssignments provided by the group leader (will only be present if
	// the group is stable).
	//
	// See consumer groups section of https://cwiki.apache.org/confluence/display/KAFKA/A+Guide+To+The+Kafka+Protocol
	MemberAssignments []byte
}

func (t describeGroupsResponseMemberV0) size() int32 {
	return sizeofString(t.MemberID) +
		sizeofString(t.ClientID) +
		sizeofString(t.ClientHost) +
		sizeofBytes(t.MemberMetadata) +
		sizeofBytes(t.MemberAssignments)
}

func (t describeGroupsResponseMemberV0) writeTo(wb *writeBuffer) {
	wb.writeString(t.MemberID)
	wb.writeString(t.ClientID)
	wb.writeString(t.ClientHost)
	wb.writeBytes(t.MemberMetadata)
	wb.writeBytes(t.MemberAssignments)
}

func (t *describeGroupsResponseMemberV0) readFrom(r *bufio.Reader, size int) (remain int, err error) {
	if remain, err = readString(r, size, &t.MemberID); err != nil {
		return
	}
	if remain, err = readString(r, remain, &t.ClientID); err != nil {
		return
	}
	if remain, err = readString(r, remain, &t.ClientHost); err != nil {
		return
	}
	if remain, err = readBytes(r, remain, &t.MemberMetadata); err != nil {
		return
	}
	if remain, err = readBytes(r, remain, &t.MemberAssignments); err != nil {
		return
	}
	return
}

type describeGroupsResponseGroupV0 struct {
	// ErrorCode holds response error code
	ErrorCode int16

	// GroupID holds the unique group identifier
	GroupID string

	// State holds current state of the group (one of: Dead, Stable, AwaitingSync,
	// PreparingRebalance, or empty if there is no active group)
	State string

	// ProtocolType holds the current group protocol type (will be empty if there is
	// no active group)
	ProtocolType string

	// Protocol holds the current group protocol (only provided if the group is Stable)
	Protocol string

	// Members contains the current group members (only provided if the group is not Dead)
	Members []describeGroupsResponseMemberV0
}

func (t describeGroupsResponseGroupV0) size() int32 {
	return sizeofInt16(t.ErrorCode) +
		sizeofString(t.GroupID) +
		sizeofString(t.State) +
		sizeofString(t.ProtocolType) +
		sizeofString(t.Protocol) +
		sizeofArray(len(t.Members), func(i int) int32 { return t.Members[i].size() })
}

func (t describeGroupsResponseGroupV0) writeTo(wb *writeBuffer) {
	wb.writeInt16(t.ErrorCode)
	wb.writeString(t.GroupID)
	wb.writeString(t.State)
	wb.writeString(t.ProtocolType)
	wb.writeString(t.Protocol)
	wb.writeArray(len(t.Members), func(i int) { t.Members[i].writeTo(wb) })
}

func (t *describeGroupsResponseGroupV0) readFrom(r *bufio.Reader, size int) (remain int, err error) {
	if remain, err = readInt16(r, size, &t.ErrorCode); err != nil {
		return
	}
	if remain, err = readString(r, remain, &t.GroupID); err != nil {
		return
	}
	if remain, err = readString(r, remain, &t.State); err != nil {
		return
	}
	if remain, err = readString(r, remain, &t.ProtocolType); err != nil {
		return
	}
	if remain, err = readString(r, remain, &t.Protocol); err != nil {
		return
	}

	fn := func(r *bufio.Reader, size int) (fnRemain int, fnErr error) {
		item := describeGroupsResponseMemberV0{}
		if fnRemain, fnErr = (&item).readFrom(r, size); err != nil {
			return
		}
		t.Members = append(t.Members, item)
		return
	}
	if remain, err = readArrayWith(r, remain, fn); err != nil {
		return
	}

	return
}

type describeGroupsResponseV0 struct {
	// Groups holds selected group information
	Groups []describeGroupsResponseGroupV0
}

func (t describeGroupsResponseV0) size() int32 {
	return sizeofArray(len(t.Groups), func(i int) int32 { return t.Groups[i].size() })
}

func (t describeGroupsResponseV0) writeTo(wb *writeBuffer) {
	wb.writeArray(len(t.Groups), func(i int) { t.Groups[i].writeTo(wb) })
}

func (t *describeGroupsResponseV0) readFrom(r *bufio.Reader, sz int) (remain int, err error) {
	fn := func(r *bufio.Reader, size int) (fnRemain int, fnErr error) {
		item := describeGroupsResponseGroupV0{}
		if fnRemain, fnErr = (&item).readFrom(r, size); fnErr != nil {
			return
		}
		t.Groups = append(t.Groups, item)
		return
	}
	if remain, err = readArrayWith(r, sz, fn); err != nil {
		return
	}

	return
}

// decodeMemberMetadata converts raw metadata bytes to a
// DescribeGroupsResponseMemberMetadata struct.
func decodeMemberMetadata(rawMetadata []byte) (DescribeGroupsResponseMemberMetadata, error) {
	mm := DescribeGroupsResponseMemberMetadata{}

	if len(rawMetadata) == 0 {
		return mm, nil
	}

	buf := bytes.NewBuffer(rawMetadata)
	bufReader := bufio.NewReader(buf)
	remain := len(rawMetadata)

	var err error

	if remain, err = readInt16(bufReader, remain, &mm.Version); err != nil {
		return mm, err
	}
	if remain, err = readStringArray(bufReader, remain, &mm.Topics); err != nil {
		return mm, err
	}
	if remain, err = readBytes(bufReader, remain, &mm.UserData); err != nil {
		return mm, err
	}

	if remain != 0 {
		return mm, fmt.Errorf("Got non-zero number of bytes remaining: %d", remain)
	}

	return mm, nil
}

// decodeMemberAssignments converts raw assignment bytes to a DescribeGroupsResponseAssignments
// struct.
func decodeMemberAssignments(rawAssignments []byte) (DescribeGroupsResponseAssignments, error) {
	ma := DescribeGroupsResponseAssignments{}

	if len(rawAssignments) == 0 {
		return ma, nil
	}

	buf := bytes.NewBuffer(rawAssignments)
	bufReader := bufio.NewReader(buf)
	remain := len(rawAssignments)

	var err error

	if remain, err = readInt16(bufReader, remain, &ma.Version); err != nil {
		return ma, err
	}

	fn := func(r *bufio.Reader, size int) (fnRemain int, fnErr error) {
		item := GroupMemberTopic{
			Partitions: []int32{},
		}

		if fnRemain, fnErr = readString(r, size, &item.Topic); fnErr != nil {
			return
		}

		if fnRemain, fnErr = readInt32Array(r, fnRemain, &item.Partitions); fnErr != nil {
			return
		}

		ma.Topics = append(ma.Topics, item)
		return
	}
	if remain, err = readArrayWith(bufReader, remain, fn); err != nil {
		return ma, err
	}

	if remain, err = readBytes(bufReader, remain, &ma.UserData); err != nil {
		return ma, err
	}

	if remain != 0 {
		return ma, fmt.Errorf("Got non-zero number of bytes remaining: %d", remain)
	}

	return ma, nil
}

// readInt32Array reads an array of int32s. It's adapted from the implementation of
// readStringArray.
func readInt32Array(r *bufio.Reader, sz int, v *[]int32) (remain int, err error) {
	var content []int32
	fn := func(r *bufio.Reader, size int) (fnRemain int, fnErr error) {
		var value int32
		if fnRemain, fnErr = readInt32(r, size, &value); fnErr != nil {
			return
		}
		content = append(content, value)
		return
	}
	if remain, err = readArrayWith(r, sz, fn); err != nil {
		return
	}

	*v = content
	return
}
