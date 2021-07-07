// Copyright 2020 ConsenSys Software Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plonk

import (
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	fiatshamir "github.com/consensys/gnark-crypto/fiat-shamir"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr/kzg"

	bn254witness "github.com/consensys/gnark/internal/backend/bn254/witness"
)

var (
	errWrongClaimedQuotient = errors.New("claimed quotient is not as expected")
)

func Verify(proof *Proof, vk *VerifyingKey, publicWitness bn254witness.Witness) error {

	// derive gamma from Comm(l), Comm(r), Comm(o)
	fs := fiatshamir.NewTranscript(fiatshamir.SHA256, "gamma", "alpha", "zeta")
	err := fs.Bind("gamma", proof.LRO[0].Marshal())
	if err != nil {
		return err
	}
	err = fs.Bind("gamma", proof.LRO[1].Marshal())
	if err != nil {
		return err
	}
	err = fs.Bind("gamma", proof.LRO[2].Marshal())
	if err != nil {
		return err
	}
	bgamma, err := fs.ComputeChallenge("gamma")
	if err != nil {
		return err
	}
	var gamma fr.Element
	gamma.SetBytes(bgamma)

	// derive alpha from Comm(l), Comm(r), Comm(o), Com(Z)
	err = fs.Bind("alpha", proof.Z.Marshal())
	if err != nil {
		return err
	}
	balpha, err := fs.ComputeChallenge("alpha")
	if err != nil {
		return err
	}
	var alpha fr.Element
	alpha.SetBytes(balpha)

	// derive zeta, the point of evaluation
	err = fs.Bind("zeta", proof.H[0].Marshal())
	if err != nil {
		return err
	}
	err = fs.Bind("zeta", proof.H[1].Marshal())
	if err != nil {
		return err
	}
	err = fs.Bind("zeta", proof.H[2].Marshal())
	if err != nil {
		return err
	}
	bzeta, err := fs.ComputeChallenge("zeta")
	if err != nil {
		return err
	}
	var zeta fr.Element
	zeta.SetBytes(bzeta)

	// evaluation of Z=X**m-1 at zeta
	var zetaPowerM, zzeta, one fr.Element
	var bExpo big.Int
	one.SetOne()
	bExpo.SetUint64(vk.Size)
	zetaPowerM.Exp(zeta, &bExpo)
	zzeta.Sub(&zetaPowerM, &one)

	// ccompute PI = Sum_i<n L_i*w_i
	// TODO use batch inversion
	var pi, den, acc, lagrange, lagrangeOne, xiLi fr.Element
	lagrange.Set(&zzeta) // zeta**m-1
	acc.SetOne()
	den.Sub(&zeta, &acc)
	lagrange.Div(&lagrange, &den).Mul(&lagrange, &vk.SizeInv) // 1/n*(zeta**n-1)/(zeta-1)
	lagrangeOne.Set(&lagrange)                                // save it for later
	for i := 0; i < len(publicWitness); i++ {

		xiLi.Mul(&lagrange, &publicWitness[i])
		pi.Add(&pi, &xiLi)

		// use L_i+1 = w*Li*(X-z**i)/(X-z**i+1)
		lagrange.Mul(&lagrange, &vk.Generator).
			Mul(&lagrange, &den)
		acc.Mul(&acc, &vk.Generator)
		den.Sub(&zeta, &acc)
		lagrange.Div(&lagrange, &den)
	}

	// linearizedpolynomial + pi(zeta) + (Z(u*zeta))*(a+s1+gamma)*(b+s2+gamma)*(c+gamma)*alpha - alpha**2*L1(zeta)
	var _s1, _s2, _o, alphaSquareLagrange fr.Element

	zu := proof.ZShiftedOpening.ClaimedValue

	claimedQuotient := proof.BatchedProof.ClaimedValues[0]
	linearizedPolynomialZeta := proof.BatchedProof.ClaimedValues[1]
	l := proof.BatchedProof.ClaimedValues[2]
	r := proof.BatchedProof.ClaimedValues[3]
	o := proof.BatchedProof.ClaimedValues[4]
	s1 := proof.BatchedProof.ClaimedValues[5]
	s2 := proof.BatchedProof.ClaimedValues[6]

	_s1.Add(&l, &s1).Add(&_s1, &gamma) // (a+s1+gamma)
	_s2.Add(&r, &s2).Add(&_s2, &gamma) // (b+s2+gamma)
	_o.Add(&o, &gamma)                 // (c+gamma)

	_s1.Mul(&_s1, &_s2).
		Mul(&_s1, &_o).
		Mul(&_s1, &alpha).
		Mul(&_s1, &zu) // alpha*Z(u*zeta)*(a+s1+gamma)*(b+s2+gamma)*(c+gamma)

	alphaSquareLagrange.Mul(&lagrangeOne, &alpha).
		Mul(&alphaSquareLagrange, &alpha) // alpha**2*L1(zeta)
	linearizedPolynomialZeta.Add(&linearizedPolynomialZeta, &pi). // linearizedpolynomial + pi(zeta)
									Add(&linearizedPolynomialZeta, &_s1).                // linearizedpolynomial+pi(zeta)+alpha*Z(u*zeta)*(a+s1+gamma)*(b+s2+gamma)*(c+gamma)
									Sub(&linearizedPolynomialZeta, &alphaSquareLagrange) // linearizedpolynomial+pi(zeta)+(Z(u*zeta))*(a+s1+gamma)*(b+s2+gamma)*(c+gamma)*alpha-alpha**2*L1(zeta)

	// Compute H(zeta) using the previous result: H(zeta) = prev_result/(zeta**n-1)
	var zetaPowerMMinusOne fr.Element
	zetaPowerMMinusOne.Sub(&zetaPowerM, &one)
	linearizedPolynomialZeta.Div(&linearizedPolynomialZeta, &zetaPowerMMinusOne)

	// check that H(zeta) is as claimed
	if !claimedQuotient.Equal(&linearizedPolynomialZeta) {
		return errWrongClaimedQuotient
	}

	// compute the folded commitment to H: Comm(h1) + zeta**m*Comm(h2) + zeta**2m*Comm(h3)
	mPlusTwo := big.NewInt(int64(vk.Size) + 2)
	var zetaMPlusTwo fr.Element
	zetaMPlusTwo.Exp(zeta, mPlusTwo)
	var zetaMPlusTwoBigInt big.Int
	zetaMPlusTwo.ToBigIntRegular(&zetaMPlusTwoBigInt)
	foldedH := proof.H[2]
	foldedH.ScalarMultiplication(&foldedH, &zetaMPlusTwoBigInt)
	foldedH.Add(&foldedH, &proof.H[1])
	foldedH.ScalarMultiplication(&foldedH, &zetaMPlusTwoBigInt)
	foldedH.Add(&foldedH, &proof.H[0])

	// Compute the commitment to the linearized polynomial
	// first part: individual constraints
	// TODO clean that part, lots of copy / use of Affine coordinates
	var lb, rb, ob, rlb big.Int
	var rl fr.Element
	l.ToBigIntRegular(&lb)
	r.ToBigIntRegular(&rb)
	o.ToBigIntRegular(&ob)
	rl.Mul(&l, &r).ToBigIntRegular(&rlb)
	linearizedPolynomialDigest := vk.Ql
	linearizedPolynomialDigest.ScalarMultiplication(&linearizedPolynomialDigest, &lb) //l*ql
	tmp := vk.Qr
	tmp.ScalarMultiplication(&tmp, &rb)
	linearizedPolynomialDigest.Add(&linearizedPolynomialDigest, &tmp) // l*ql+r*qr
	tmp = vk.Qm
	tmp.ScalarMultiplication(&tmp, &rlb)
	linearizedPolynomialDigest.Add(&linearizedPolynomialDigest, &tmp) // l*ql+r*qr+rl*qm
	tmp = vk.Qo
	tmp.ScalarMultiplication(&tmp, &ob)
	linearizedPolynomialDigest.Add(&linearizedPolynomialDigest, &tmp) // l*ql+r*qr+rl*qm+o*qo
	tmp = vk.Qk
	linearizedPolynomialDigest.Add(&linearizedPolynomialDigest, &tmp) // l*ql+r*qr+rl*qm+o*qo+qk

	// second part: alpha*( Z(uzeta)(a+s1+gamma)*(b+s2+gamma)*s3(X)-Z(X)(a+zeta+gamma)*(b+uzeta+gamma)*(c+u**2*zeta+gamma) )
	var t fr.Element
	_s1.Add(&l, &s1).Add(&_s1, &gamma)
	t.Add(&r, &s2).Add(&t, &gamma)
	_s1.Mul(&_s1, &t).
		Mul(&_s1, &zu).
		Mul(&_s1, &alpha) // alpha*(Z(uzeta)(a+s1+gamma)*(b+s2+gamma))
	_s2.Add(&l, &zeta).Add(&_s2, &gamma)
	t.Mul(&zeta, &vk.Shifter[0]).Add(&t, &r).Add(&t, &gamma)
	_s2.Mul(&t, &_s2)
	t.Mul(&zeta, &vk.Shifter[1]).Add(&t, &o).Add(&t, &gamma)
	_s2.Mul(&t, &_s2).
		Mul(&_s2, &alpha) // alpha*(a+zeta+gamma)*(b+uzeta+gamma)*(c+u**2*zeta+gamma)
	var _s1b, _s2b big.Int
	_s1.ToBigIntRegular(&_s1b)
	_s2.ToBigIntRegular(&_s2b)
	s3Commit := vk.S[2]
	s3Commit.ScalarMultiplication(&s3Commit, &_s1b)
	secondPart := proof.Z
	secondPart.ScalarMultiplication(&secondPart, &_s2b)
	secondPart.Sub(&s3Commit, &secondPart)

	// third part: alpha**2*L1(zeta)*Z
	var alphaSquareLagrangeB big.Int
	alphaSquareLagrange.ToBigIntRegular(&alphaSquareLagrangeB)
	thirdPart := proof.Z
	thirdPart.ScalarMultiplication(&thirdPart, &alphaSquareLagrangeB)

	// finish the computation
	linearizedPolynomialDigest.Add(&linearizedPolynomialDigest, &secondPart).
		Add(&linearizedPolynomialDigest, &thirdPart)

	// verify the opening proofs
	err = kzg.BatchVerifySinglePoint(
		[]kzg.Digest{
			foldedH,
			linearizedPolynomialDigest,
			proof.LRO[0],
			proof.LRO[1],
			proof.LRO[2],
			vk.S[0],
			vk.S[1],
		},
		&proof.BatchedProof,
		vk.KZGSRS,
	)
	if err != nil {
		return err
	}

	return kzg.Verify(&proof.Z, &proof.ZShiftedOpening, vk.KZGSRS)
}
