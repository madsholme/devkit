FROM python:3.13-slim
ARG DEVKIT_COMMIT
ENV DEVKIT_COMMIT=$DEVKIT_COMMIT
WORKDIR /app
COPY app.py message.txt ./
EXPOSE 8000
CMD ["python", "app.py"]
