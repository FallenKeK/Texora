package main

// eqTestSet — образцы формул для проверки раскладки и кругового обмена (go run . eqtest и eq_test.go).
var eqTestSet = []string{
	`A = \begin{pmatrix} a_{11} & a_{12} & a_{13} \\ a_{21} & a_{22} & a_{23} \\ a_{31} & a_{32} & a_{33} \end{pmatrix}`,
	`\det A = \begin{vmatrix} a & b \\ c & d \end{vmatrix} = ad - bc`,
	`\begin{cases} x + y = 5, \\ 2x - y = 1 \end{cases}`,
	`f(x) = \begin{cases} x^2, & x \ge 0 \\ -x, & x < 0 \end{cases}`,
	`(a+b)^2 &= (a+b)(a+b) \\ &= a^2 + ab + ba + b^2 \\ &= a^2 + 2ab + b^2`,
	`\dv[2]{y}{x} + \omega^2 y = 0, \quad \pdv{u}{t} = a^2 \pdv[2]{u}{x}`,
	`\lim_{x \to 0} \frac{\sin x}{x} = 1, \quad \binom{n}{k} = \frac{n!}{k!(n-k)!}`,
	`\overbrace{1 + 2 + \dots + n}^{n} = \frac{n(n+1)}{2}, \; \underbrace{a \cdot a \cdots a}_{k}`,
	`\vec{F} = m\vec{a}, \quad \left\| \vec{v} \right\| = \sqrt{v_x^2 + v_y^2}, \quad \left\langle u, v \right\rangle`,
	`x \in \mathbb{R}, \; \mathbf{M} \mathbf{x} = \mathbf{b}, \; \overset{\text{def}}{=} \; \forall \varepsilon > 0 \; \exists \delta`,
	`\int_0^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2} \tag{1.3}`,
	`\left\lfloor \frac{n}{2} \right\rfloor + \left\lceil \frac{n}{2} \right\rceil = n`,
}
