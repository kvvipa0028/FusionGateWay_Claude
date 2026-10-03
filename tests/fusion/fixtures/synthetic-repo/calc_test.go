package calculator

import "testing"

func TestAdd(t *testing.T) {
 for _, c := range []struct{a,b,want int}{{2,3,5},{-2,3,1},{0,0,0}} {
  if got:=Add(c.a,c.b); got!=c.want { t.Errorf("Add(%d,%d)=%d, want %d",c.a,c.b,got,c.want) }
 }
}
