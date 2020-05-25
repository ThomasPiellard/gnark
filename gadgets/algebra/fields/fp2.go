package fields

import (
	"github.com/consensys/gnark/frontend"
)

// Fp2Elmt element in a quadratic extension
type Fp2Elmt struct {
	x, y *frontend.Constraint
}

// NewFp2Elmt creates a fp2elmt from x, y points
func NewFp2Elmt(circuit *frontend.CS, _x, _y interface{}) Fp2Elmt {

	if _x == nil && _y == nil {
		return Fp2Elmt{nil, nil}
	}
	res := Fp2Elmt{
		x: circuit.ALLOCATE(_x),
		y: circuit.ALLOCATE(_y),
	}
	return res
}

// Add e2 elmts
func (e *Fp2Elmt) Add(circuit *frontend.CS, e1, e2 *Fp2Elmt) *Fp2Elmt {
	x := circuit.ADD(e1.x, e2.x)
	y := circuit.ADD(e1.y, e2.y)
	e.x = x
	e.y = y
	return e
}

// Sub e2 elmts
func (e *Fp2Elmt) Sub(circuit *frontend.CS, e1, e2 *Fp2Elmt) *Fp2Elmt {
	x := circuit.SUB(e1.x, e2.x)
	y := circuit.SUB(e1.y, e2.y)
	e.x = x
	e.y = y
	return e
}

// Mul e2 elmts
func (e *Fp2Elmt) Mul(circuit *frontend.CS, e1, e2 *Fp2Elmt, ext Extension) *Fp2Elmt {
	a := circuit.MUL(e1.x, e2.x)
	b := circuit.MUL(e1.y, e2.y)
	b = circuit.MUL(b, ext.uSquare)
	x := circuit.ADD(a, b)

	c := circuit.MUL(e1.x, e2.y)
	d := circuit.MUL(e1.y, e2.x)
	y := circuit.ADD(c, d)

	e.x = x
	e.y = y

	return e
}

// MulByFp multiplies an fp2 elmt by an fp elmt
func (e *Fp2Elmt) MulByFp(circuit *frontend.CS, e1 *Fp2Elmt, c interface{}) *Fp2Elmt {
	e.x = circuit.MUL(e1.x, c)
	e.y = circuit.MUL(e1.y, c)
	return e
}

// MulByIm multiplies an fp2 elmt by the imaginary elmt
// ext.uSquare is the square of the imaginary root
func (e *Fp2Elmt) MulByIm(circuit *frontend.CS, e1 *Fp2Elmt, ext Extension) *Fp2Elmt {
	x := e1.y
	e.x = circuit.MUL(e.y, ext.uSquare)
	e.y = x
	return e
}

// Conjugate conjugation of an e2 elmt
func (e *Fp2Elmt) Conjugate(circuit *frontend.CS, e1 *Fp2Elmt) *Fp2Elmt {
	e.x = e1.x
	e.y = circuit.SUB(0, e1.y)
	return e
}
