// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cosi implements Ed25519 collective signatures.
// A collective signature allows many participants to
// validate and sign a message collaboratively,
// to produce a single compact multisignature that can be verified
// almost as quickly and efficiently as a single individual signature.
// Despite their compactness, collective signatures nevertheless
// record exactly which subset of participants signed a given message,
// to tolerate unavailable participants and support arbitrary policies
// defining the required thresholds or subsets of signers
// required to produce a collective signature considered acceptable.
// For further background information on collective signatures, see the paper
// http://dedis.cs.yale.edu/dissent/papers/witness-abs.
//
// This package implements the basic cryptographic operations needed
// to create and/or verify collective signatures using the Ed25519 curve.
// This package does not provide a full protocol
// to create collective signatures, however.
// An implementation of CoSi,
// the scalable collective signing protocol described in the above paper,
// may be found at https://github.com/dedis/cothority.
// We recommend using CoSi to produce collective signatures in practice,
// especially if you may eventually need to scale
// to hundreds or thousands of cosigners.
// It is possible to "hand-roll" a basic collective signing protocol
// using only the cryptographic primitives implemented in this package, however.
//
// In practice, we expect this package to be used mostly 
// for verification of signatures generated by the CoSi protocol,
// in the context of client applications relying on collective signatures.
// Verifying already-generated collective signatures requires
// only the code in this package.
//
// Public Keys for Collective Signing and Verification
//
// In a conventional signing scheme such as basic Ed25519,
// the individual signer uses a public key to sign a message,
// and verifiers use the corresponding private key to check its validity.
// Collective signing involves using a set of key pairs:
// the holders of multiple distinct private keys collaborate to sign a message,
// and to verify the resulting collective signature,
// the verifier needs to have a list of the corresponding public keys.
// The key-management considerations are mostly the same
// as for standard individual signing:
// the verifier must have reason to believe the public key list is trustworthy,
// e.g., by having obtained it from a trusted source or certificate authority.
// The difference is that the verifier need to assume that the holder
// of any single corresponding private key is trustworthy.
// Even if one or a few key-holders are compromised,
// these compromised key-holders cannot forge valid messages
// unless they meet whatever threshold requirement the verifier demands.
//
// This collective signature module uses
// exactly the same public and private keys as basic ed25519 does:
// thus, you simply use ed25519.GenerateKey to produce keypairs
// suitable for collective signing using this module.
// The Cosigners type implemented by this package
// represents a set of cosigners identified by their ed25519 public keys:
// you create such a set by calling NewCosigners with the list of public keys.
//
// The order of this public key list is arbitrary,
// but must be kept consistent between signing and verifying.
// Since not all participants will necessarily
// participate in every message signing operation,
// each collective signature includes a bit-mask indicating any cosigners
// that were missing (e.g., offline) during the production of the signature.
// Each public key in the master cosigner list corresponds
// to one bit in this "absentee" bitmask,
// in corresponding order,
// so that verifiers can tell exactly which cosigners actually signed.
// The bitmask is cryptographically bound into the signature,
// so the signature will fail to verify if someone merely flips a bit
// in attempt to pretend that an absent participant
// in fact cosigned the message.
// 
// Although key-management security considerations are mostly the same
// as for individual signing schemes,
// collective signing does add one important detail to be aware of.
// In the process of collecting a set of public keys to form a cosigning group,
// if those public keys originate from mutually-distrustful parties,
// as is often desirable to maximize the security and diversity of the group,
// then it is critical that each party's public-key be self-signed.
// That is, each member must verify that every other group member
// actually knows the private key corresponding to his claimed public key.
// This is standard practice anyway in both public-key infrastructure (PKI)
// and "peer-to-peer" key management as implemented by PGP for example.
// This practice becomes even more essential in collective signing, however,
// because if a malicious participant is allowed to "claim" any public key
// without proviing knowledge of its corresponding private key,
// then the participant can use so-called "related-key attacks"
// to produce signatures that appear to be signed by other group members
// but in fact were signed only by the one malicious signer.
// For further details, see the CoSi paper above,
// as well as section 3.2 of this paper:
// http://cs-www.bu.edu/~reyzin/papers/multisig.pdf.
//
// Verifying Collective Signatures
//
// Verifying collective signatures is simple,
// and may be done offline at any time without any special protocol.
// Simply use NewCosigners to create a Cosigners object
// representing the list of cosigners identified by their public keys,
// then invoke the Verify method on this object
// to verify a signature on a particular message.
// The Verify function returns true if the collective signature is valid,
// and changes the state of the mask in the Cosigners object
// to indicate which cosigners were present or absent
// in the production of this particular collective signature.
//
// Besides checking the cryptographic validity of the signature itself,
// the Verify function also invokes a customizable policy
// to check whether the actual set of cosigners that produced the signature
// is acceptable to the verifier.
// The (conservative) default policy is that every cosigner must have signed
// in order for the collective signature signature to be considered valid.
// The verifier can adjust this policy by invoking Cosigners.SetPolicy
// before invoking Verify on the signature.
// The ThresholdPolicy function may be used to form policies that
// simply require a given threshold number of signers to have cosigned.
// The caller may express an arbitrary policy, however,
// simply by passing SetPolicy an object implementing the Policy interface.
// Such a Policy can depend in any way on the set of participating cosigners,
// as well as other state such as the particular verification context
// (e.g., how security-critical an operation the signature is being used for).
//
// Note that a collective signature in which no signers actually participated
// can technically be a valid collective signature,
// and will be accepted if the verifier calls SetPolicy(ThresholdPolicy(0))!
// This merely illustrates the importance of
// choosing the verification policy carefully.
//
// Producing Collective Signatures
//
// Although as mentioned above we recommend using a scalable protocol
// such as CoSi to produce collective signatures in practice,
// collective signatures can also be produced
// using the signing primitives in this package.
// Collective signing is more complex than verification or individual signing
// because the collective signers must collaborate actively in the process.
// The process works as follows:
//
// 1. Some party we'll call the "leader"
// initiates the collective signing process.
// The leader could be any one of the cosigners,
// or any other designated (or elected) party.
// The leader need not hold any of the cosigners' private keys.
// The leader determines which cosigners appear to be online,
// and sends them the message to be collectively signed.
//
// 2. Each cosigner first inspects the message the leader asked to be signed,
// using message-validation logic suitable to the application.
// Cosigners need not necessarily validate the message at all
// if their purpose is merely to provide transparency
// by publicly "witnessing" the signing of the message.
// If the cosigner is willing to sign,
// it calls the Commit function to produce a signing commitment,
// returning this commitment to the leader
// along with an indication of the cosigner's willingness to participate.
// Commitments may be used only once (for signing a particular message),
// an important security property this package strictly enforces.
//
// 3. The leader adjusts the participation mask in its Cosigners object
// to reflect the set of cosigners that are online and willing to cosign.
// The leader then calls Cosigners.AggregateCommits
// to combine the willing cosigners' commitments together,
// and sends the resulting aggregate commit to all the cosigners.
//
// 4. Each cosigner now calls the Cosign function -
// the only function in this package requiring the cosigner's PrivateKey -
// to produce its portion or "share" of the collective signature.
// The cosigner sends this signature part back to the leader.
//
// 5. Finally, the leader invokes Cosigners.AggregateSignature
// to combine the participating cosigners' signature parts
// into a full collective signature.
// The resulting collective signature may subsequently checked
// by anyone using Cosigners.Verify function as described above,
// on a Cosigners object created from an identical list of public keys.
//
// The leader must keep the participation mask in its Cosigners object
// fixed between steps 2 and 4 above.
// If any cosigner indicates willingness in step 2
// but then changes its mind or goes offline before step 4,
// the leader must restart the signing process with an adjusted mask.
// This restart risk could be eliminated, at certain costs,
// using mechanisms not implemented in this package;
// see the CoSi paper for details.
//
// While collecting signature parts in step 4,
// the leader can verify each cosigner's individual signature part
// independently using Cosigners.VerifyPart.
// This way, if any cosigner indicates willingness to participate
// but actually produces an invalid signature part -
// whether due to software bugs or malice -
// the leader can determine which cosigner is responsible, 
// raise an alarm, and restart the signing process without that cosigner.
// If VerifyPart indicates each individual signature part is valid,
// then the final collective signature produced by AggregateSignature
// will also be valid (unless the leader itself is buggy).
//
// Efficiency Considerations
//
// Each Cosigners object caches some cryptographic state -
// namely the aggregate public key returned by AggregatePublicKey -
// reflecting the current participation bitmask.
// The SetMask and SetMaskBit functions, which change the participation bitmask,
// updates the cached cryptographic state accordingly.
// As a result, both collective signing and verification operations
// are maximally efficient when a single Cosigners object is used
// multiple times in succession using the same, or a similar,
// participation bitmask.
//
// Drastically changing the bitmask therefore incurs some computational cost.
// This cost is unlikely to be particular noticeable
// unless the total number of cosigners' public keys is quite large, however
// (e.g., thousands),
// because updating the cached aggregate public key requires only
// an elliptic curve point addition or subtraction operation per cosigner.
// Point addition and subtraction operations are extremely inexpensive
// compared to the scalar multiplication operations that represent
// a constant cost in collective signing or verification,
// so these constant costs will typically dominate
// whene the list of cosigners is small.
package cosi

// This code is a port of the public domain, “ref10” implementation of ed25519
// from SUPERCOP.

import (
	cryptorand "crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"io"
	"strconv"
	"math/big"
	//"encoding/hex"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ed25519/internal/edwards25519"
)


// MaskBit represents one bit of a Cosigners participation bitmask,
// indicating whether a given cosigner is Enabled or Disabled.
type MaskBit bool

const (
	Enabled MaskBit = false
	Disabled MaskBit = true
)


// Commitment represents a byte-slice used in the collective signing process,
// which cosigners produce via Commit and send to the leader
// for combination via AggregateCommit.
type Commitment []byte

// SignaturePart represents a byte-slice used in collective signing,
// which cosigners produce via Cosign and send to the leader
// for combination via AggregateSignature.
type SignaturePart []byte


// Policy represents a fully customizable cosigning policy
// deciding what cosigner sets are and aren't sufficient
// for a collective signature to be considered acceptable to a verifier.
// The Check method may inspect the set of participants that cosigned
// by invoking cosigners.Mask and/or cosigners.MaskBit,
// and may use any other relevant contextual information
// (e.g., how security-critical
// the operation relying on the collective signature is)
// in determining whether the collective signature
// was produced by an acceptable set of cosigners.
type Policy interface {
	Check(cosigners *Cosigners) bool
}

// The default, conservative policy
// just requires all participants to have signed.
type fullPolicy struct{}
func (_ fullPolicy) Check(cosigners *Cosigners) bool {
	return cosigners.CountEnabled() == cosigners.CountTotal()
}

type thresPolicy struct{ t int }
func (p thresPolicy) Check(cosigners *Cosigners) bool {
	return cosigners.CountEnabled() >= p.t
}

// ThresholdPolicy creates a Policy object representing a simple T-of-N policy,
// which deems a collective signature acceptable provided
// that at least the given threshold number of participants cosigned.
func ThresholdPolicy(threshold int) Policy {
	return &thresPolicy{threshold}
}

// XXX add simple threshold policy


// Secret represents a one-time random secret used
// in collectively signing a single message.
type Secret struct {
	reduced [32]byte
	valid bool
}

// Commit is invoked by cosigners to produce a one-time commit
// to be used in the collective signing of a single message.
// Producing this commit requires fresh cryptographically random bits,
// which are taken from rand, or from a default source if rand is nil.
//
// On success, Commit returns the commit as a byte-slice
// to be sent to the leader for aggregation via AggregateCommit,
// and a Secret object representing a cryptographic secret
// to be used later in the corresponding call to Cosign.
// Commit fails and returns an error only if rand yields an error.
func Commit(rand io.Reader) (Commitment, *Secret, error) {

	var secretFull [64]byte
	if rand == nil {
		rand = cryptorand.Reader
	}
	_, err := io.ReadFull(rand, secretFull[:])
	if err != nil {
		return nil, nil, err
	}

	var secret Secret
	edwards25519.ScReduce(&secret.reduced, &secretFull)
	secret.valid = true

	// compute R, the individual Schnorr commit to our one-time secret
	var R edwards25519.ExtendedGroupElement
	edwards25519.GeScalarMultBase(&R, &secret.reduced)

	var encodedR [32]byte
	R.ToBytes(&encodedR)
	return encodedR[:], &secret, nil
}

// Cosign signs the message with privateKey and returns a partial signature. It will
// panic if len(privateKey) is not PrivateKeySize.

// Cosign is used by a cosigner to produce its part of a collective signature.
// This operation requires the cosigner's private key,
// the local per-message Secret previously produced
// by the corresponding call to Commit,
// and the aggregate public key and aggregate commit
// that the leader obtained in this signing round
// from AggregatePublicKey and AggregateCommit respectively.
//
// Since it is security-critical that a particular Secret be used only once,
// Cosign invalidates the secret when it is called,
// and panics if called with a previously-used secret.
func Cosign(privateKey ed25519.PrivateKey, secret *Secret, message []byte,
	aggregateK ed25519.PublicKey, aggregateR Commitment) SignaturePart {

	if l := len(privateKey); l != ed25519.PrivateKeySize {
		panic("ed25519: bad private key length: " + strconv.Itoa(l))
	}
	if l := len(aggregateR); l != ed25519.PublicKeySize {
		panic("ed25519: bad aggregateR length: " + strconv.Itoa(l))
	}
	if !secret.valid {
		panic("ed25519: you must use a cosigning Secret only once")
	}

	h := sha512.New()
	h.Write(privateKey[:32])

	var digest1 [64]byte
	var expandedSecretKey [32]byte
	h.Sum(digest1[:0])
	copy(expandedSecretKey[:], digest1[:])
	expandedSecretKey[0] &= 248
	expandedSecretKey[31] &= 63
	expandedSecretKey[31] |= 64

	var hramDigest [64]byte
	h.Reset()
	h.Write(aggregateR)
	h.Write(aggregateK)
	h.Write(message)
	h.Sum(hramDigest[:0])

	var hramDigestReduced [32]byte
	edwards25519.ScReduce(&hramDigestReduced, &hramDigest)

	// Produce our individual contribution to the collective signature
	var s [32]byte
	edwards25519.ScMulAdd(&s, &hramDigestReduced, &expandedSecretKey,
				&secret.reduced)

	// Erase the one-time secret and make darn sure it gets used only once,
	// even if a buggy caller invokes Cosign twice after a single Commit
	secret.reduced = [32]byte{}
	secret.valid = false

	return s[:]	// individual partial signature
}


// Cosigners represents a group of collective signers
// identified by an immutable, ordered list of their public keys.
// In addition, the Cosigners object includes a mutable bitmask
// indicating which cosigners are to participate in a signing operation,
// and which cosigners actually participated when verifying a signature.
// Finally, a Cosigners object contains a customizable Policy
// that determines what subsets of cosigners are and aren't acceptable
// when verifying a collective signature.
//
// Since a Cosigners object contains mutable fields
// and implements no thread-safety provisions internally,
// a given Cosigners instance must be used only by one thread at a time.
type Cosigners struct {
	// list of all cosigners' public keys in internalized form
	keys []edwards25519.ExtendedGroupElement

	// bit-vector of *disabled* cosigners
	mask big.Int

	// cached aggregate of all enabled cosigners' public keys
	aggr edwards25519.ExtendedGroupElement

	// cosigner-presence policy for checking signatures
	policy Policy
}

// NewCosigners creates a new Cosigners object
// for a particular list of cosigners identified by Ed25519 public keys.
// The specified list of public keys remains immutable
// for the lifetime of this Cosigners object.
// Collective signature verifiers must use a public key list identical
// to the one that was used in the collective signing process,
// although the participation bitmask may change
// from one collective signature to the next.
func NewCosigners(publicKeys []ed25519.PublicKey) *Cosigners {
	var publicKeyBytes [32]byte
	cos := &Cosigners{}
	cos.keys = make([]edwards25519.ExtendedGroupElement, len(publicKeys))
	for i, publicKey := range publicKeys {
		copy(publicKeyBytes[:], publicKey)
		if !cos.keys[i].FromBytes(&publicKeyBytes) {
			return nil
		}
	}
	cos.SetMask(nil)
	cos.policy = &fullPolicy{}
	return cos
}

// CountTotal returns the total number of cosigners,
// i.e., the length of the list of public keys supplied to NewCosigners.
func (cos *Cosigners) CountTotal() int {
	return len(cos.keys)
}

// CountEnabled returns the number of participants currently marked Enabled
// in the participation bitmask.
// This is always between 0 and CountTotal inclusive.
func (cos *Cosigners) CountEnabled() int {
	// Yes, we could count zero-bits much more efficiently...
	count := 0
	for i := range cos.keys {
		if cos.MaskBit(i) == Enabled {
			count++
		}
	}
	return count
}

//func (cos *Cosigners) PublicKeys() []ed25519.PublicKey {
//	return cos.keys
//}

// SetMask sets the entire participation bitmask according to the provided
// packed byte-slice interpreted in little-endian byte-order.
// That is, bits 0-7 of the first byte correspond to cosigners 0-7,
// bits 0-7 of the next byte correspond to cosigners 8-15, etc.
// Each bit is set to indicate the corresponding cosigner is disabled,
// or cleared to indicate the cosigner is enabled.
//
// If the mask provided is too short (or nil),
// SetMask conservatively interprets the bits of the missing bytes
// to be 0, or Enabled.
func (cos *Cosigners) SetMask(mask []byte) {
	cos.mask.SetInt64(0)
	cos.aggr.Zero()
	masklen := len(mask)
	for i := range cos.keys {
		if (i>>3 < masklen) && (mask[i>>3] & (1 << uint(i&7)) != 0) {
			cos.mask.SetBit(&cos.mask, i, 1)	// disable
		} else {
			cos.aggr.Add(&cos.aggr, &cos.keys[i])	// enable
		}
	}
}

// Mask returns the current cosigner disable-mask
// represented a byte-packed little-endian bit-vector.
func (cos *Cosigners) Mask() []byte {
	mask := make([]byte, (len(cos.keys)+7)>>3)
	for i := 0; i < len(cos.keys); i++ {
		if cos.mask.Bit(i) > 0 {
			mask[i>>3] |= 1 << uint(i&7)
		}
	}
	return mask
}

// MaskLen returns the length in bytes
// of a complete disable-mask for this cosigner list.
func (cos *Cosigners) MaskLen() int {
	return (len(cos.keys)+7) >> 3
}

// SetMaskBit enables or disables the mask bit for an individual cosigner.
func (cos *Cosigners) SetMaskBit(signer int, bit MaskBit) {
	if bit == Disabled {				// disable
		if cos.mask.Bit(signer) == 0 {		// was enabled
			cos.mask.SetBit(&cos.mask, signer, 1)
			cos.aggr.Sub(&cos.aggr, &cos.keys[signer])
		}
	} else {					// enable
		if cos.mask.Bit(signer) == 1 {		// was disabled
			cos.mask.SetBit(&cos.mask, signer, 0)
			cos.aggr.Add(&cos.aggr, &cos.keys[signer])
		}
	}
}

// MaskBit returns a boolean value indicating whether
// the indicated signer is Enabled or Disabled.
func (cos *Cosigners) MaskBit(signer int) (bit MaskBit) {
	return cos.mask.Bit(signer) != 0
}

// AggregatePublicKey computes and returns an aggregate public key
// representing the set of cosigners
// currently enabled in the participation bitmask.
// The leader invokes this method during collective signing
// to determine the aggregate public key that needs to be passed
// to the cosigners and supplied to their Cosign operations.
func (cos *Cosigners) AggregatePublicKey() ed25519.PublicKey {
	var keyBytes [32]byte
	cos.aggr.ToBytes(&keyBytes)
	return keyBytes[:]
}

// AggregateCommit is invoked by the leader during collective signing
// to combine all cosigners' individual commits into an aggregate commit,
// which it must pass back to all cosigners for use in their Cosign operations.
// The commits slice must have length equal to the total number of cosigners,
// but AggregateCommit uses only the entries corresponding to cosigners
// that are enabled in the participation mask.
func (cos *Cosigners) AggregateCommit(commits []Commitment) []byte {

	var aggR, indivR edwards25519.ExtendedGroupElement
	var commitBytes [32]byte

	aggR.Zero()
	for i := range cos.keys {
		if cos.MaskBit(i) == Disabled {
			continue
		}

		if l := len(commits[i]); l != ed25519.PublicKeySize {
			return nil
		}
		copy(commitBytes[:], commits[i])
		if !indivR.FromBytes(&commitBytes) {
			return nil
		}
		aggR.Add(&aggR, &indivR)
	}

	var aggRBytes [32]byte
	aggR.ToBytes(&aggRBytes)
	return aggRBytes[:]
}

var scOne = [32]byte{1}

// AggregateSignature is invoked by the leader during collective signing
// to combine all cosigners' individual signature parts
// into a final collective signature.
// The sigParts slice must have length equal to the total number of cosigners,
// but AggregateSignature uses only the entries corresponding to cosigners
// that are enabled in the participation mask,
// which must be identical to the one
// the leader previously used during AggregateCommit.
func (cos *Cosigners) AggregateSignature(aggregateR Commitment, sigParts []SignaturePart) []byte {

	if l := len(aggregateR); l != ed25519.PublicKeySize {
		panic("ed25519: bad aggregateR length: " + strconv.Itoa(l))
	}

	var aggS, indivS [32]byte
	for i := range cos.keys {
		if cos.MaskBit(i) == Disabled {
			continue
		}

		if l := len(sigParts[i]); l != 32 {
			return nil
		}
		copy(indivS[:], sigParts[i])
		edwards25519.ScMulAdd(&aggS, &aggS, &scOne, &indivS)
	}

	mask := cos.Mask()
	cosigSize := ed25519.SignatureSize + len(mask)
	signature := make([]byte, cosigSize)
	copy(signature[:], aggregateR)
	copy(signature[32:64], aggS[:])
	copy(signature[64:], mask)

	return signature
}

// VerifyPart allows the leader to verify an individual cosigner's
// signature part during collective signing.
// This allows the leader to detect if a buggy or malicious cosigner
// produces an invalid signature part
// that might render the final collective signature unusable.
// In such a situation, the leader cannot complete this signing round,
// but can restart the collective signing process (with new commits)
// after excluding the buggy or malicious cosigner.
func (cos *Cosigners) VerifyPart(message, aggR Commitment,
				signer int, indR, indS []byte) bool {

	return cos.verify(message, aggR, indR, indS, cos.keys[signer])
}

// Verify determines whether collective signature represented by sig
// is a valid collective signature on the indicated message,
// collectively signed by an acceptable set of cosigners.
// Whether the set of participating cosigners is acceptable
// is determined by the currently-registered Policy.
//
// The default policy conservatively requires all cosigners to participate
// in order for Verify to deem the collective signature acceptable,
// but this policy may be changed by calling SetPolicy
// before invoking Verify.
//
// Verify changes the Cosigners object's participation bitmask
// to the mask carried in the verified signature,
// before invoking the Policy object.
// Thus, a custom Policy can use Cosigners.MaskBit and/or Cosigners.Mask
// to inspect the set of cosigners that actually signed the message,
// and determine the acceptability of the collective signature
// on the basis of the participation mask
// and any other relevant contextual information.
// In addition, after Verify returns,
// the caller can similarly inspect the resulting participation mask
// to determine which specific cosigners did and did not sign.
//
func (cos *Cosigners) Verify(message, sig []byte) bool {

	cosigSize := ed25519.SignatureSize + cos.MaskLen()
	if len(sig) != cosigSize {
		return false
	}

	// Update our mask to reflect which cosigners actually signed
	cos.SetMask(sig[64:])

	// Check that this prepresents a sufficient set of signers
	if !cos.policy.Check(cos) {
		return false
	}

	return cos.verify(message, sig[:32], sig[:32], sig[32:64], cos.aggr)
}

func (cos *Cosigners) verify(message, aggR, sigR, sigS []byte,
		sigA edwards25519.ExtendedGroupElement) bool {

	if len(sigR) != 32 || len(sigS) != 32 || sigS[31]&224 != 0 {
		return false
	}

	// Compute the digest against aggregate public key and commit
	var aggK [32]byte
	cos.aggr.ToBytes(&aggK)

	h := sha512.New()
	h.Write(aggR)
	h.Write(aggK[:])
	h.Write(message)
	var digest [64]byte
	h.Sum(digest[:0])

	var hReduced [32]byte
	edwards25519.ScReduce(&hReduced, &digest)

	// The public key used for checking is whichever part was signed
	edwards25519.FeNeg(&sigA.X, &sigA.X)
	edwards25519.FeNeg(&sigA.T, &sigA.T)

	var projR edwards25519.ProjectiveGroupElement
	var b [32]byte
	copy(b[:], sigS)
	edwards25519.GeDoubleScalarMultVartime(&projR, &hReduced, &sigA, &b)

	var checkR [32]byte
	projR.ToBytes(&checkR)
	return subtle.ConstantTimeCompare(sigR, checkR[:]) == 1
}

// SetPolicy changes the current Policy object registered
// for this Cosigners object,
// which is used by Verify to determine the acceptability
// of the participant set indicated in a particular collective signature.
// The default policy in any new Cosigners object
// conservatively requires all cosigners to participate in every signature.
// Standard 'T-of-N' threshold-signing policies may be obtained
// by passing a Policy object produced by the ThresholdPolicy function.
// More exotic, arbitrarily customized policies may be used
// by passing any object that implements the Policy interface.
func (cos *Cosigners) SetPolicy(policy Policy) {
	cos.policy = policy	
}

