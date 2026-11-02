import { useEffect } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import Guard from './components/Guard'
import Layout from './components/Layout'
import { useAuth } from './store/auth'
import AdminUsersPage from './pages/AdminUsersPage'
import ChangePasswordPage from './pages/ChangePasswordPage'
import LoginPage from './pages/LoginPage'
import UploadPage from './pages/UploadPage'
import UploadsPage from './pages/UploadsPage'

export default function App() {
  const check = useAuth((s) => s.check)
  useEffect(() => {
    void check()
  }, [check])

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route element={<Guard />}>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/change-password" element={<ChangePasswordPage />} />
            <Route path="/" element={<UploadPage />} />
            <Route path="/uploads" element={<UploadsPage />} />
            <Route path="/admin/users" element={<AdminUsersPage />} />
            <Route path="/admin/uploads" element={<UploadsPage all />} />
            <Route path="*" element={<p>Page not found.</p>} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
