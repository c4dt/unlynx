package libunlynx_test

import (
	"github.com/lca1/unlynx/lib"
	"github.com/stretchr/testify/assert"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/crypto.v0/random"
	"gopkg.in/dedis/onet.v1/network"
	"reflect"
	"testing"
)

var suite = network.Suite

// TestNullCipherText verifies encryption, decryption and behavior of null ciphertexts.
func TestNullCipherText(t *testing.T) {

	secKey, pubKey := libunlynx.GenKey()

	nullEnc := libunlynx.EncryptInt(pubKey, 0)
	nullDec := libunlynx.DecryptInt(secKey, *nullEnc)

	if 0 != nullDec {
		t.Fatal("Decryption of encryption of 0 should be 0, got", nullDec)
	}

	var twoTimesNullEnc = libunlynx.CipherText{K: suite.Point().Null(), C: suite.Point().Null()}
	twoTimesNullEnc.Add(*nullEnc, *nullEnc)
	twoTimesNullDec := libunlynx.DecryptInt(secKey, twoTimesNullEnc)

	if 0 != nullDec {
		t.Fatal("Decryption of encryption of 0+0 should be 0, got", twoTimesNullDec)
	}

}

// TestEncryption tests a relatively high number of encryptions.
func TestEncryption(t *testing.T) {

	_, pubKey := libunlynx.GenKey()

	nbrEncryptions := 2
	for i := 0; i < nbrEncryptions; i++ {
		libunlynx.EncryptInt(pubKey, 0)
	}
}

// TestDecryptionConcurrent test the multiple encryptions/decryptions at the same time
func TestDecryptionConcurrent(t *testing.T) {
	numThreads := 5

	sec, pubKey := libunlynx.GenKey()

	libunlynx.StartParallelize(numThreads)

	for i := 0; i < numThreads; i++ {
		go func() {
			ct := libunlynx.EncryptInt(pubKey, 0)
			val := libunlynx.DecryptInt(sec, *ct)
			assert.Equal(t, val, int64(0))
		}()
	}
}

// TestNullCipherText verifies encryption, decryption and behavior of null cipherVectors.
func TestNullCipherVector(t *testing.T) {
	secKey, pubKey := libunlynx.GenKey()

	nullVectEnc := *libunlynx.NullCipherVector(10, pubKey)
	nullVectDec := libunlynx.DecryptIntVector(secKey, &nullVectEnc)

	target := []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if !reflect.DeepEqual(nullVectDec, target) {
		t.Fatal("Null vector of dimension 4 should be ", target, "got", nullVectDec)
	}

	twoTimesNullEnc := libunlynx.NewCipherVector(10)
	twoTimesNullEnc.Add(nullVectEnc, nullVectEnc)
	twoTimesNullDec := libunlynx.DecryptIntVector(secKey, twoTimesNullEnc)

	if !reflect.DeepEqual(twoTimesNullDec, target) {
		t.Fatal("Null vector + Null vector should be ", target, "got", twoTimesNullDec)
	}
}

// TestHomomorphicOpp tests homomorphic addition.
func TestHomomorphicOpp(t *testing.T) {
	secKey, pubKey := libunlynx.GenKey()

	cv1 := libunlynx.EncryptIntVector(pubKey, []int64{0, 1, 2, 3, 100})
	cv2 := libunlynx.EncryptIntVector(pubKey, []int64{0, 0, 1, 3, 3})
	targetAdd := []int64{0, 1, 3, 6, 103}
	targetSub := []int64{0, 1, 1, 0, 97}
	targetMul := int64(4)

	cv3 := libunlynx.NewCipherVector(5)
	cv3.Add(*cv1, *cv2)
	cv4 := libunlynx.NewCipherVector(5)
	cv4.Sub(*cv1, *cv2)
	cv5 := libunlynx.EncryptInt(pubKey, 2)
	cv5.MulCipherTextbyScalar(*cv5, suite.Scalar().SetInt64(2))

	pAdd := libunlynx.DecryptIntVector(secKey, cv3)
	pSub := libunlynx.DecryptIntVector(secKey, cv4)
	pMul := libunlynx.DecryptInt(secKey, *cv5)

	assert.Equal(t, targetAdd, pAdd)
	assert.Equal(t, targetSub, pSub)
	assert.Equal(t, targetMul, pMul)
}

// TestCryptoTagging tests the deterministic tagging
func TestCryptoTagging(t *testing.T) {
	const N = 5

	groupKey, private, _ := libunlynx.GenKeys(N)
	_, secretPrivate, _ := libunlynx.GenKeys(N)

	target := []int64{-8358645081376817152, -8358645081376817152, 2, 3, 2, 5}
	cv := *libunlynx.EncryptIntVector(groupKey, target)
	for n := 0; n < N; n++ {
		tmp := libunlynx.NewCipherVector(len(cv))
		tmp.DeterministicTagging(&cv, private[n], secretPrivate[n])

		cv = *tmp

	}
	assert.True(t, cv[0].C.Equal(cv[1].C))
	assert.True(t, cv[2].C.Equal(cv[4].C))
	assert.False(t, cv[0].C.Equal(cv[3].C))
}

// TestCryptoKeySwitching tests key switching.
func TestCryptoKeySwitching(t *testing.T) {
	const N = 5
	groupKey, privates, _ := libunlynx.GenKeys(N)
	newPrivate, newPublic := libunlynx.GenKey()

	target := []int64{1, 2, 3, 4, 5}
	cv := libunlynx.EncryptIntVector(groupKey, target)

	origEphem := make([]abstract.Point, len(*cv))
	kscv := make(libunlynx.CipherVector, len(*cv))
	for i, c := range *cv {
		origEphem[i] = c.K
		kscv[i].K = suite.Point().Null()
		kscv[i].C = c.C
	}

	for n := 0; n < N; n++ {
		kscv.KeySwitching(kscv, origEphem, newPublic, privates[n])
	}

	res := libunlynx.DecryptIntVector(newPrivate, &kscv)
	assert.True(t, reflect.DeepEqual(res, target))

}

// TestEqualDeterministCipherText tests equality between deterministic ciphertexts.
func TestEqualDeterministCipherText(t *testing.T) {
	dcv1 := libunlynx.DeterministCipherVector{libunlynx.DeterministCipherText{Point: suite.Point().Base()}, libunlynx.DeterministCipherText{Point: suite.Point().Null()}}
	dcv2 := libunlynx.DeterministCipherVector{libunlynx.DeterministCipherText{Point: suite.Point().Base()}, libunlynx.DeterministCipherText{Point: suite.Point().Null()}}

	assert.True(t, dcv1.Equal(&dcv2))
	assert.True(t, dcv1.Equal(&dcv1))

	dcv1 = libunlynx.DeterministCipherVector{}
	dcv2 = libunlynx.DeterministCipherVector{}
	assert.True(t, dcv1.Equal(&dcv2))
	assert.True(t, dcv1.Equal(&dcv1))

	var nilp *libunlynx.DeterministCipherVector
	pdcv1 := &dcv1
	assert.True(t, pdcv1.Equal(&dcv2))
	assert.False(t, pdcv1.Equal(nilp))

	pdcv1 = nil
	assert.False(t, pdcv1.Equal(&dcv2))
	assert.True(t, pdcv1.Equal(nilp))
}

// TestAbstractPointsConverter tests the abstract points array converter (to bytes)
func TestAbstractPointsConverter(t *testing.T) {
	aps := make([]abstract.Point, 0)

	clientPrivate := network.Suite.Scalar().Pick(random.Stream)

	for i := 0; i < 4; i++ {
		ap := network.Suite.Point().Mul(network.Suite.Point().Base(), clientPrivate)
		aps = append(aps, ap)
	}

	aps_bytes := libunlynx.AbstractPointsToBytes(aps)
	new_aps := libunlynx.BytesToAbstractPoints(aps_bytes)

	for i, el := range aps {
		if !reflect.DeepEqual(el.String(), new_aps[i].String()) {
			t.Fatal("Wrong results, expected", el, "but got", new_aps[i])
		}
	}

	t.Log("[AbstractPoints] -> Good results")
}

// TestCiphertextConverter tests the Ciphertext converter (to bytes)
func TestCiphertextConverter(t *testing.T) {
	secKey, pubKey := libunlynx.GenKey()

	target := int64(2)
	ct := libunlynx.EncryptInt(pubKey, target)

	ctb := ct.ToBytes()

	new_ct := libunlynx.CipherText{}
	new_ct.FromBytes(ctb)

	p := libunlynx.DecryptInt(secKey, new_ct)

	assert.Equal(t, target, p)
}

// TestCipherVectorConverter tests the CipherVector converter (to bytes)
func TestCipherVectorConverter(t *testing.T) {
	secKey, pubKey := libunlynx.GenKey()

	target := []int64{0, 1, 3, 103, 103}
	cv := libunlynx.EncryptIntVector(pubKey, target)

	cvb, length := cv.ToBytes()

	new_cv := libunlynx.CipherVector{}
	new_cv.FromBytes(cvb, length)

	p := libunlynx.DecryptIntVector(secKey, &new_cv)

	assert.Equal(t, target, p)
}

// TestIntArrayToCipherVector tests the int array to CipherVector converter and IntToPoint + PointToCiphertext
func TestIntArrayToCipherVector(t *testing.T) {
	integers := []int64{1, 2, 3, 4, 5, 6}

	cipherVect := libunlynx.IntArrayToCipherVector(integers)
	for i, v := range cipherVect {
		B := suite.Point().Base()
		i := suite.Scalar().SetInt64(integers[i])
		M := suite.Point().Mul(B, i)
		N := suite.Point().Null()
		assert.Equal(t, v.C, M)
		assert.Equal(t, v.K, N)
	}
}

func TestB64Serialization(t *testing.T) {
	secKey, pubKey := libunlynx.GenKey()
	target := []int64{0, 1, 3, 103, 103}
	cv := libunlynx.EncryptIntVector(pubKey, target)

	for i, ct := range *cv {
		ctSerialized := ct.Serialize()

		// with newciphertext
		ctDeserialized := libunlynx.NewCipherTextFromBase64(ctSerialized)
		decVal := libunlynx.DecryptInt(secKey, *ctDeserialized)
		assert.Equal(t, target[i], decVal)

		// with deserialize
		ctDeserializedBis := libunlynx.NewCipherText()
		ctDeserializedBis.Deserialize(ctSerialized)
		decValBis := libunlynx.DecryptInt(secKey, *ctDeserializedBis)
		assert.Equal(t, target[i], decValBis)
		assert.Equal(t, decVal, decValBis)
	}
}
