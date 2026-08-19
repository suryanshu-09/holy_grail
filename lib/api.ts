export async function health() {
  const res = await fetch('http://localhost:8080/health');
  return res.json();
}
