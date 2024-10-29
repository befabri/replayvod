import { useRouteError } from "react-router-dom";

interface ErrorType {
  statusText?: string;
}

interface ErrorType {
  message?: string;
}

export default function ErrorPage() {
  const error = useRouteError();
  console.error(error);
  let errorMessage: string | undefined;
  if (typeof error === 'object' && error !== null) {
    const typedError = error as ErrorType;
    errorMessage = typedError.statusText || typedError.message;
  }
  
  return (
    <div id="error-page">
      <h1>Oops!</h1>
      <p>Sorry, an unexpected error has occurred.</p>
      <p>
        <i>{errorMessage}</i>
      </p>
    </div>
  );
}