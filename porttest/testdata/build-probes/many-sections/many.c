// Seventy thousand functions, each in a section of its own, so that the
// object has more sections than an ELF header's e_shnum can count.
#define F(n) __attribute__((section(".text.s" #n))) int s##n(void) { return 0; }
#define D1(n) F(n##0) F(n##1) F(n##2) F(n##3) F(n##4) F(n##5) F(n##6) F(n##7) F(n##8) F(n##9)
#define D2(n) D1(n##0) D1(n##1) D1(n##2) D1(n##3) D1(n##4) D1(n##5) D1(n##6) D1(n##7) D1(n##8) D1(n##9)
#define D3(n) D2(n##0) D2(n##1) D2(n##2) D2(n##3) D2(n##4) D2(n##5) D2(n##6) D2(n##7) D2(n##8) D2(n##9)
#define D4(n) D3(n##0) D3(n##1) D3(n##2) D3(n##3) D3(n##4) D3(n##5) D3(n##6) D3(n##7) D3(n##8) D3(n##9)
D4(1) D4(2) D4(3) D4(4) D4(5) D4(6) D4(7)
