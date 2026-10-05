package evidence

import "testing"

func TestGoReportRequiresCompleteBalancedActualCases(t *testing.T) {
	start := `{"Action":"start","Package":"."}` + "\n"
	run := `{"Action":"run","Package":".","Test":"TestReal"}` + "\n"
	pass := `{"Action":"pass","Package":".","Test":"TestReal","Elapsed":0.001}` + "\n"
	end := `{"Action":"pass","Package":".","Elapsed":0.001}` + "\n"
	if c, e := parseGoReport([]byte(start+run+pass+end), "."); e != nil || c.tests != 1 || c.failures != 0 {
		t.Fatal(c, e)
	}
	for _, data := range []string{run + pass + end, start + run + pass, start + run + end, start + pass + end, start + run + run + pass + end, start + run + pass + end + end, start + run + `{"Action":"magic","Package":".","Test":"TestReal"}` + end, start + `{"Action":"pass","Package":"foreign"}`, start + `{"Action":"pass","Package":".","Unknown":true}`, start + `{"Action":"pass","Package":".","Elapsed":-1}`, start + `{"Action":"fail","Action":"pass","Package":"."}`} {
		if _, e := parseGoReport([]byte(data), "."); e == nil {
			t.Fatal("malformed or incomplete report passed", data)
		}
	}
}
