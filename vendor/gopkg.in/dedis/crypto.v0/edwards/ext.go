package edwards

import (
	"crypto/cipher"
	"encoding/hex"
	"io"
	"math/big"

	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/crypto.v0/group"
	"gopkg.in/dedis/crypto.v0/nist"
)

type extPoint struct {
	X, Y, Z, T nist.Int
	c          *ExtendedCurve
}

func (P *extPoint) initXY(x, y *big.Int, c abstract.Group) {
	P.c = c.(*ExtendedCurve)
	P.X.Init(x, &P.c.P)
	P.Y.Init(y, &P.c.P)
	P.Z.Init64(1, &P.c.P)
	P.T.Mul(&P.X, &P.Y)
}

func (P *extPoint) getXY() (x, y *nist.Int) {
	P.normalize()
	return &P.X, &P.Y
}

func (P *extPoint) String() string {
	P.normalize()
	//return P.c.pointString(&P.X,&P.Y)
	buf, _ := P.MarshalBinary()
	return hex.EncodeToString(buf)
}

func (P *extPoint) MarshalSize() int {
	return P.c.PointLen()
}

func (P *extPoint) MarshalBinary() ([]byte, error) {
	P.normalize()
	return P.c.encodePoint(&P.X, &P.Y), nil
}

func (P *extPoint) UnmarshalBinary(b []byte) error {
	if err := P.c.decodePoint(b, &P.X, &P.Y); err != nil {
		return err
	}
	P.Z.Init64(1, &P.c.P)
	P.T.Mul(&P.X, &P.Y)
	return nil
}

func (P *extPoint) MarshalTo(w io.Writer) (int, error) {
	return group.PointMarshalTo(P, w)
}

func (P *extPoint) UnmarshalFrom(r io.Reader) (int, error) {
	return group.PointUnmarshalFrom(P, r)
}

func (P *extPoint) HideLen() int {
	return P.c.hide.HideLen()
}

func (P *extPoint) HideEncode(rand cipher.Stream) []byte {
	return P.c.hide.HideEncode(P, rand)
}

func (P *extPoint) HideDecode(rep []byte) {
	P.c.hide.HideDecode(P, rep)
}

// Equality test for two Points on the same curve.
// We can avoid inversions here because:
//
//	(X1/Z1,Y1/Z1) == (X2/Z2,Y2/Z2)
//		iff
//	(X1*Z2,Y1*Z2) == (X2*Z1,Y2*Z1)
//
func (P1 *extPoint) Equal(CP2 abstract.Point) bool {
	P2 := CP2.(*extPoint)
	var t1, t2 nist.Int
	xeq := t1.Mul(&P1.X, &P2.Z).Equal(t2.Mul(&P2.X, &P1.Z))
	yeq := t1.Mul(&P1.Y, &P2.Z).Equal(t2.Mul(&P2.Y, &P1.Z))
	return xeq && yeq
}

func (P *extPoint) Set(CP2 abstract.Point) abstract.Point {
	P2 := CP2.(*extPoint)
	P.c = P2.c
	P.X.Set(&P2.X)
	P.Y.Set(&P2.Y)
	P.Z.Set(&P2.Z)
	P.T.Set(&P2.T)
	return P
}

func (P *extPoint) Clone() abstract.Point {
	return &extPoint{
		c: P.c,
		X: P.X,
		Y: P.Y,
		Z: P.Z,
		T: P.T,
	}
}

func (P *extPoint) Null() abstract.Point {
	P.Set(&P.c.null)
	return P
}

func (P *extPoint) Base() abstract.Point {
	P.Set(&P.c.base)
	return P
}

func (P *extPoint) PickLen() int {
	return P.c.pickLen()
}

// Normalize the point's representation to Z=1.
func (P *extPoint) normalize() {
	P.Z.Inv(&P.Z)
	P.X.Mul(&P.X, &P.Z)
	P.Y.Mul(&P.Y, &P.Z)
	P.Z.V.SetInt64(1)
	P.T.Mul(&P.X, &P.Y)
}

// Check the validity of the T coordinate
func (P *extPoint) checkT() {
	var t1, t2 nist.Int
	if !t1.Mul(&P.X, &P.Y).Equal(t2.Mul(&P.Z, &P.T)) {
		panic("oops")
	}
}

func (P *extPoint) Pick(data []byte, rand cipher.Stream) (abstract.Point, []byte) {
	leftover := P.c.pickPoint(P, data, rand)
	return P, leftover
}

// Extract embedded data from a point group element
func (P *extPoint) Data() ([]byte, error) {
	P.normalize()
	return P.c.data(&P.X, &P.Y)
}

// Add two points using optimized extended coordinate addition formulas.
func (P *extPoint) Add(CP1, CP2 abstract.Point) abstract.Point {
	P1 := CP1.(*extPoint)
	P2 := CP2.(*extPoint)
	X1, Y1, Z1, T1 := &P1.X, &P1.Y, &P1.Z, &P1.T
	X2, Y2, Z2, T2 := &P2.X, &P2.Y, &P2.Z, &P2.T
	X3, Y3, Z3, T3 := &P.X, &P.Y, &P.Z, &P.T
	var A, B, C, D, E, F, G, H nist.Int

	A.Mul(X1, X2)
	B.Mul(Y1, Y2)
	C.Mul(T1, T2).Mul(&C, &P.c.d)
	D.Mul(Z1, Z2)
	E.Add(X1, Y1).Mul(&E, F.Add(X2, Y2)).Sub(&E, &A).Sub(&E, &B)
	F.Sub(&D, &C)
	G.Add(&D, &C)
	H.Mul(&P.c.a, &A).Sub(&B, &H)
	X3.Mul(&E, &F)
	Y3.Mul(&G, &H)
	T3.Mul(&E, &H)
	Z3.Mul(&F, &G)
	return P
}

// Subtract points.
func (P *extPoint) Sub(CP1, CP2 abstract.Point) abstract.Point {
	P1 := CP1.(*extPoint)
	P2 := CP2.(*extPoint)
	X1, Y1, Z1, T1 := &P1.X, &P1.Y, &P1.Z, &P1.T
	X2, Y2, Z2, T2 := &P2.X, &P2.Y, &P2.Z, &P2.T
	X3, Y3, Z3, T3 := &P.X, &P.Y, &P.Z, &P.T
	var A, B, C, D, E, F, G, H nist.Int

	A.Mul(X1, X2)
	B.Mul(Y1, Y2)
	C.Mul(T1, T2).Mul(&C, &P.c.d)
	D.Mul(Z1, Z2)
	E.Add(X1, Y1).Mul(&E, F.Sub(Y2, X2)).Add(&E, &A).Sub(&E, &B)
	F.Add(&D, &C)
	G.Sub(&D, &C)
	H.Mul(&P.c.a, &A).Add(&B, &H)
	X3.Mul(&E, &F)
	Y3.Mul(&G, &H)
	T3.Mul(&E, &H)
	Z3.Mul(&F, &G)
	return P
}

// Find the negative of point A.
// For Edwards curves, the negative of (x,y) is (-x,y).
func (P *extPoint) Neg(CA abstract.Point) abstract.Point {
	A := CA.(*extPoint)
	P.c = A.c
	P.X.Neg(&A.X)
	P.Y.Set(&A.Y)
	P.Z.Set(&A.Z)
	P.T.Neg(&A.T)
	return P
}

// Optimized point doubling for use in scalar multiplication.
// Uses the formulae in section 3.3 of:
// https://www.iacr.org/archive/asiacrypt2008/53500329/53500329.pdf
func (P *extPoint) double() {
	X1, Y1, Z1, T1 := &P.X, &P.Y, &P.Z, &P.T
	var A, B, C, D, E, F, G, H nist.Int

	A.Mul(X1, X1)
	B.Mul(Y1, Y1)
	C.Mul(Z1, Z1).Add(&C, &C)
	D.Mul(&P.c.a, &A)
	E.Add(X1, Y1).Mul(&E, &E).Sub(&E, &A).Sub(&E, &B)
	G.Add(&D, &B)
	F.Sub(&G, &C)
	H.Sub(&D, &B)
	X1.Mul(&E, &F)
	Y1.Mul(&G, &H)
	T1.Mul(&E, &H)
	Z1.Mul(&F, &G)
}

// Multiply point p by scalar s using the repeated doubling method.
//
// Currently doesn't implement the optimization of
// switching between projective and extended coordinates during
// scalar multiplication.
//
func (P *extPoint) Mul(G abstract.Point, s abstract.Scalar) abstract.Point {
	v := s.(*nist.Int).V
	if G == nil {
		return P.Base().Mul(P, s)
	}
	T := P
	if G == P { // Must use temporary for in-place multiply
		T = &extPoint{}
	}
	T.Set(&P.c.null) // Initialize to identity element (0,1)
	for i := v.BitLen() - 1; i >= 0; i-- {
		T.double()
		if v.Bit(i) != 0 {
			T.Add(T, G)
		}
	}
	if T != P {
		P.Set(T)
	}
	return P
}

// ExtendedCurve implements Twisted Edwards curves
// using projective coordinate representation (X:Y:Z),
// satisfying the identities x = X/Z, y = Y/Z.
// This representation still supports all Twisted Edwards curves
// and avoids expensive modular inversions on the critical paths.
// Uses the projective arithmetic formulas in:
// http://cr.yp.to/newelliptic/newelliptic-20070906.pdf
//

// ExtendedCurve implements Twisted Edwards curves
// using the Extended Coordinate representation specified in:
// Hisil et al, "Twisted Edwards Curves Revisited",
// http://eprint.iacr.org/2008/522
//
// This implementation is designed to work with all Twisted Edwards curves,
// foregoing the further optimizations that are available for the
// special case with curve parameter a=-1.
// We leave the task of hyperoptimization to curve-specific implementations
// such as the ed25519 package.
//
type ExtendedCurve struct {
	curve          // generic Edwards curve functionality
	null  extPoint // Constant identity/null point (0,1)
	base  extPoint // Standard base point
}

// Create a new Point on this curve.
func (c *ExtendedCurve) Point() abstract.Point {
	P := new(extPoint)
	P.c = c
	//P.Set(&c.null)
	return P
}

// Initialize the curve with given parameters.
func (c *ExtendedCurve) Init(p *Param, fullGroup bool) *ExtendedCurve {
	c.curve.init(c, p, fullGroup, &c.null, &c.base)
	return c
}
